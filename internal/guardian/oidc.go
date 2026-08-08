package guardian

import (
	"context"
	"crypto/rand"
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
// Guardian's /oauth/authorize only honors a service's exact registered
// redirect URIs; dynamic/localhost callbacks (registered as "extra redirect
// URI patterns") are honored exclusively via PAR (RFC 9126) + /auth/login.
// So the CLI flow is:
//
//  1. POST /api/v1/oauth/par with client_id + redirect_uri + state
//     -> single-use request_uri (90s TTL)
//  2. open the browser at /auth/login?client_id=...&request_uri=...
//  3. Guardian runs SSO and redirects back to the localhost callback with
//     ?token=<access JWT>&state=<state> - no code exchange step.
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
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", cfg.CallbackPort)

	// Callback listener — bind before starting the flow so we can't miss
	// the redirect.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on callback port %d: %w", cfg.CallbackPort, err)
	}
	defer listener.Close()

	type callbackResult struct {
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
		token := q.Get("token")
		if token == "" {
			http.Error(w, "Missing token", http.StatusBadRequest)
			resultChan <- callbackResult{err: errors.New("callback missing token")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body style="font-family: sans-serif; text-align: center; padding-top: 4rem;">
<h2>Login successful</h2><p>You can close this tab and return to the terminal.</p>
</body></html>`)
		resultChan <- callbackResult{token: token}
	})}
	go server.Serve(listener)
	defer server.Close()

	// Step 1: PAR — push the authorization request, get a request_uri.
	// This is what grants the localhost redirect URI provenance; direct
	// /auth/login query params only match exact registered URIs.
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

	// Step 2: send the browser to /auth/login with the opaque request_uri.
	// Note the request_uri is single-use with a ~90s TTL — it's consumed as
	// soon as the browser hits /auth/login, so a slow SSO after that is fine.
	loginURL := fmt.Sprintf("%s/auth/login?client_id=%s&request_uri=%s",
		baseURL, url.QueryEscape(cfg.ClientID), url.QueryEscape(par.RequestURI))

	if cfg.AuthURLCallback != nil {
		cfg.AuthURLCallback(loginURL)
	}
	if cfg.OpenBrowser != nil {
		_ = cfg.OpenBrowser(loginURL) // failure is fine; URL was shown
	}

	// Step 3: wait for the token to arrive on the callback.
	select {
	case res := <-resultChan:
		if res.err != nil {
			return nil, res.err
		}
		return &TokenResult{AccessToken: res.token}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for login: %w", ctx.Err())
	}
}

func randomURLSafe(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
