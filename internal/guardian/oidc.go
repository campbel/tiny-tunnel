package guardian

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LoginConfig configures the browser SSO login flow used by the CLI.
//
// Guardian requires the OIDC authorization-code flow with PKCE (S256) for
// downstream services (non-code downstream logins on /auth/login are rejected
// under Custodian Session Families). Dynamic/localhost callbacks (registered
// as "extra redirect URI patterns") are honored via PAR (RFC 9126).
// So the CLI flow is:
//
//  1. POST /api/v1/oauth/par with client_id + redirect_uri + state
//     -> single-use request_uri (90s TTL)
//  2. open the browser at /oauth/authorize?client_id=...&request_uri=...
//     &response_type=code&scope=openid&code_challenge=...&code_challenge_method=S256
//  3. Guardian runs SSO and redirects back to the localhost callback with
//     ?code=<auth code>&state=<state>
//  4. POST /oauth/token with grant_type=authorization_code + code +
//     redirect_uri + client_id + code_verifier -> access_token.
type LoginConfig struct {
	// GuardianURL is the Guardian base URL, e.g. https://id.stable.dexus.io
	GuardianURL string
	// ClientID is the registered service client ID, e.g. svc_tiny-tunnel_stable
	ClientID string
	// CallbackPort is the localhost port for the redirect URI. The redirect
	// URI http://localhost:<port>/auth/callback must be registered as an
	// extra redirect URI pattern on the service in Guardian.
	CallbackPort int
	// OpenBrowser is called with the login URL. If nil or it errors, the URL
	// is expected to be presented to the user by the caller via
	// AuthURLCallback.
	OpenBrowser func(url string) error
	// AuthURLCallback, if set, receives the login URL (for printing).
	AuthURLCallback func(url string)
	// Timeout bounds the whole flow (default 5 minutes).
	Timeout time.Duration
	// HTTPClient overrides the client used for the PAR call.
	HTTPClient *http.Client
}

// TokenResult is the outcome of a successful login.
type TokenResult struct {
	AccessToken string
}

// Login runs the PAR + browser SSO flow and returns the Guardian access
// token delivered to the localhost callback.
func Login(ctx context.Context, cfg LoginConfig) (*TokenResult, error) {
	if cfg.GuardianURL == "" || cfg.ClientID == "" {
		return nil, errors.New("guardian url and client id are required")
	}
	if cfg.CallbackPort == 0 {
		cfg.CallbackPort = 8085
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	baseURL := strings.TrimSuffix(cfg.GuardianURL, "/")

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	state, err := randomURLSafe(16)
	if err != nil {
		return nil, err
	}
	codeVerifier, codeChallenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", cfg.CallbackPort)

	// Callback listener — bind before starting the flow so we can't miss
	// the redirect.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on callback port %d: %w", cfg.CallbackPort, err)
	}
	defer listener.Close()

	type callbackResult struct {
		code  string
		token string
		err   error
	}
	resultChan := make(chan callbackResult, 1)

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if errCode := q.Get("error"); errCode != "" {
			http.Error(w, "Login failed: "+errCode, http.StatusBadRequest)
			resultChan <- callbackResult{err: fmt.Errorf("authorization failed: %s (%s)", errCode, q.Get("error_description"))}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			resultChan <- callbackResult{err: errors.New("state mismatch in callback")}
			return
		}
		code := q.Get("code")
		token := q.Get("token")
		if code == "" && token == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			resultChan <- callbackResult{err: errors.New("callback missing authorization code")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body style="font-family: sans-serif; text-align: center; padding-top: 4rem;">
<h2>Login successful</h2><p>You can close this tab and return to the terminal.</p>
</body></html>`)
		resultChan <- callbackResult{code: code, token: token}
	})}
	go server.Serve(listener)
	defer server.Close()

	// Step 1: PAR — push the authorization request, get a request_uri.
	// This is what grants the localhost redirect URI provenance; direct
	// /oauth/authorize query params only match exact registered URIs.
	form := url.Values{
		"client_id":    {cfg.ClientID},
		"redirect_uri": {redirectURI},
		"state":        {state},
	}
	parReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/oauth/par", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	parReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parResp, err := httpClient.Do(parReq)
	if err != nil {
		return nil, fmt.Errorf("par request: %w", err)
	}
	defer parResp.Body.Close()

	if parResp.StatusCode != http.StatusCreated && parResp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.NewDecoder(parResp.Body).Decode(&e)
		return nil, fmt.Errorf("par request failed (%d): %s %s", parResp.StatusCode, e.Error, e.Description)
	}

	var par struct {
		RequestURI string `json:"request_uri"`
		ExpiresIn  int    `json:"expires_in"`
	}
	if err := json.NewDecoder(parResp.Body).Decode(&par); err != nil {
		return nil, fmt.Errorf("decode par response: %w", err)
	}
	if par.RequestURI == "" {
		return nil, errors.New("par response has no request_uri")
	}

	// Step 2: send the browser to /oauth/authorize with the opaque request_uri
	// and PKCE S256 challenge. Note the request_uri is single-use with a ~90s
	// TTL — it's consumed as soon as the browser hits /oauth/authorize, so a
	// slow SSO after that is fine.
	loginURL := fmt.Sprintf("%s/oauth/authorize?client_id=%s&request_uri=%s&response_type=code&scope=openid&code_challenge=%s&code_challenge_method=S256",
		baseURL,
		url.QueryEscape(cfg.ClientID),
		url.QueryEscape(par.RequestURI),
		url.QueryEscape(codeChallenge),
	)

	if cfg.AuthURLCallback != nil {
		cfg.AuthURLCallback(loginURL)
	}
	if cfg.OpenBrowser != nil {
		_ = cfg.OpenBrowser(loginURL) // failure is fine; URL was shown
	}

	// Step 3: wait for the authorization code to arrive on the callback, then
	// exchange it for an access token at /oauth/token.
	select {
	case res := <-resultChan:
		if res.err != nil {
			return nil, res.err
		}
		if res.code != "" {
			return ExchangeCode(ctx, httpClient, baseURL, cfg.ClientID, redirectURI, res.code, codeVerifier)
		}
		return &TokenResult{AccessToken: res.token}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for login: %w", ctx.Err())
	}
}

// GeneratePKCE generates an RFC 7636 PKCE code_verifier (43-char base64url)
// and its corresponding S256 code_challenge.
func GeneratePKCE() (verifier string, challenge string, err error) {
	verifier, err = randomURLSafe(32)
	if err != nil {
		return "", "", err
	}
	return verifier, S256Challenge(verifier), nil
}

// S256Challenge computes the RFC 7636 S256 code_challenge for a code_verifier.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ExchangeCode redeems a single-use OIDC authorization code at Guardian's
// POST /oauth/token endpoint using PKCE and returns the resulting access token.
func ExchangeCode(ctx context.Context, httpClient *http.Client, guardianURL, clientID, redirectURI, code, codeVerifier string) (*TokenResult, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	baseURL := strings.TrimSuffix(guardianURL, "/")
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {codeVerifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return nil, fmt.Errorf("token exchange failed (%d): %s %s", resp.StatusCode, e.Error, e.Description)
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if out.AccessToken == "" {
		return nil, errors.New("token response has no access_token")
	}
	return &TokenResult{AccessToken: out.AccessToken}, nil
}

func randomURLSafe(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
