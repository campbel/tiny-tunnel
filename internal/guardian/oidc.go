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

// LoginConfig configures the OIDC authorization-code + PKCE login flow used
// by the CLI. Guardian is a public-client IdP (token_endpoint_auth_methods =
// ["none"]) so no client secret is involved; PKCE S256 is mandatory.
type LoginConfig struct {
	// GuardianURL is the Guardian base URL, e.g. https://id.stable.dexus.io
	GuardianURL string
	// ClientID is the registered service client ID, e.g. svc_tiny-tunnel_stable
	ClientID string
	// CallbackPort is the localhost port for the redirect URI. The redirect
	// URI http://localhost:<port>/auth/callback must be registered with the
	// service in Guardian.
	CallbackPort int
	// OpenBrowser is called with the authorization URL. If nil or it errors,
	// the URL is expected to be presented to the user by the caller via
	// AuthURLCallback.
	OpenBrowser func(url string) error
	// AuthURLCallback, if set, receives the authorization URL (for printing).
	AuthURLCallback func(url string)
	// Timeout bounds the whole flow (default 5 minutes).
	Timeout time.Duration
	// HTTPClient overrides the client used for discovery/token calls.
	HTTPClient *http.Client
}

// TokenResult is the outcome of a successful login.
type TokenResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
}

type discoveryDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// Login runs the full authorization-code + PKCE flow: starts a localhost
// callback listener, opens the browser to Guardian's authorize endpoint, and
// exchanges the returned code for tokens.
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

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Discovery
	disc, err := discover(ctx, httpClient, cfg.GuardianURL)
	if err != nil {
		return nil, err
	}

	// PKCE material
	verifier, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state, err := randomURLSafe(16)
	if err != nil {
		return nil, err
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", cfg.CallbackPort)

	// Callback listener — bind before opening the browser so we can't miss
	// the redirect.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on callback port %d: %w", cfg.CallbackPort, err)
	}
	defer listener.Close()

	type callbackResult struct {
		code string
		err  error
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
		if code == "" {
			http.Error(w, "Missing code", http.StatusBadRequest)
			resultChan <- callbackResult{err: errors.New("callback missing authorization code")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body style="font-family: sans-serif; text-align: center; padding-top: 4rem;">
<h2>Login successful</h2><p>You can close this tab and return to the terminal.</p>
</body></html>`)
		resultChan <- callbackResult{code: code}
	})}
	go server.Serve(listener)
	defer server.Close()

	// Authorization URL
	authURL, err := url.Parse(disc.AuthorizationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization endpoint: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	if cfg.AuthURLCallback != nil {
		cfg.AuthURLCallback(authURL.String())
	}
	if cfg.OpenBrowser != nil {
		_ = cfg.OpenBrowser(authURL.String()) // failure is fine; URL was shown
	}

	// Wait for the callback
	var code string
	select {
	case res := <-resultChan:
		if res.err != nil {
			return nil, res.err
		}
		code = res.code
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for login: %w", ctx.Err())
	}

	// Code exchange (public client: no secret, PKCE verifier is the proof)
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {cfg.ClientID},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
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
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if out.AccessToken == "" {
		return nil, errors.New("token response has no access_token")
	}

	return &TokenResult{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		IDToken:      out.IDToken,
		ExpiresIn:    out.ExpiresIn,
	}, nil
}

func discover(ctx context.Context, httpClient *http.Client, baseURL string) (*discoveryDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery: unexpected status %d", resp.StatusCode)
	}
	var doc discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, errors.New("oidc discovery: incomplete document")
	}
	return &doc, nil
}

func randomURLSafe(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
