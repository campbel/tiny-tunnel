package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/tunneltoken"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startFakeGuardianWithLogin extends the fake Guardian with the /auth/login
// token-flow endpoint used by the device flow: it immediately "approves" the
// SSO and redirects back to the provided redirect_uri with ?token=&state=.
func startFakeGuardianWithLogin(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	guardian, mint, _ := startFakeGuardian(t)

	base, ok := guardian.Config.Handler.(*http.ServeMux)
	require.True(t, ok)
	base.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redirect, err := url.Parse(q.Get("redirect_uri"))
		require.NoError(t, err)
		rq := redirect.Query()
		rq.Set("token", mint(validClaims(guardian.URL)))
		rq.Set("state", q.Get("state"))
		redirect.RawQuery = rq.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	})
	mintValid := func() string { return mint(validClaims(guardian.URL)) }
	return guardian, mintValid
}

func newDeviceTestServer(t *testing.T, guardianURL string) *httptest.Server {
	t.Helper()
	handler := NewHandler(Options{
		Hostname:         "example.com",
		EnableAuth:       true,
		GuardianURL:      guardianURL,
		GuardianAudience: "svc_tiny-tunnel_stable",
		AccessScheme:     "http",
	}, log.NewTestLogger())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// TestDeviceFlowEndToEnd drives the full device flow: CLI starts + polls,
// "browser" opens the authorize URL and follows the Guardian redirect chain
// back to the callback, CLI receives a tnl-minted token, and that token is
// accepted on /register.
func TestDeviceFlowEndToEnd(t *testing.T) {
	guardian, _ := startFakeGuardianWithLogin(t)
	server := newDeviceTestServer(t, guardian.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	browser := make(chan string, 1) // verification_uri_complete
	resultChan := make(chan *client.DeviceLoginResult, 1)
	errChan := make(chan error, 1)

	go func() {
		result, err := client.DeviceLogin(ctx, server.URL, func(uri, uriComplete, userCode string) {
			browser <- uriComplete + "&__usercode=" + url.QueryEscape(userCode)
		})
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	// Simulate the user's browser on another machine.
	var uriComplete string
	select {
	case uriComplete = <-browser:
	case err := <-errChan:
		t.Fatalf("device login failed before prompt: %s", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for prompt")
	}

	parts := strings.SplitN(uriComplete, "&__usercode=", 2)
	pageURL := parts[0]
	userCode, _ := url.QueryUnescape(parts[1])

	// The verification URI uses the configured public hostname; rewrite to
	// the test server address.
	pageURL = strings.Replace(pageURL, "http://example.com", server.URL, 1)

	httpClient := &http.Client{} // follows redirects across servers

	// 1. Load the /device page (sanity)
	resp, err := httpClient.Get(pageURL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Submit the code -> /device/authorize -> guardian /auth/login ->
	//    /auth/callback. The callback URL also uses the public hostname, so
	//    walk the redirect chain manually.
	authorizeURL := server.URL + "/device/authorize?code=" + url.QueryEscape(userCode)
	noRedirect := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err = noRedirect.Get(authorizeURL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	guardianLogin := resp.Header.Get("Location")
	require.Contains(t, guardianLogin, "/auth/login")

	resp, err = noRedirect.Get(guardianLogin)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	callback := resp.Header.Get("Location")
	callback = strings.Replace(callback, "http://example.com", server.URL, 1)

	resp, err = httpClient.Get(callback)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "callback should render success page")

	// 3. CLI poll should now deliver the tnl token.
	var result *client.DeviceLoginResult
	select {
	case result = <-resultChan:
	case err := <-errChan:
		t.Fatalf("device login failed: %s", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for token")
	}

	require.NotEmpty(t, result.Token)
	assert.Equal(t, "ada@example.com", result.User)
	assert.True(t, tunneltoken.IsTunnelToken(result.Token), "vended credential should be a tnl-minted token")

	// 4. The vended token must be accepted for tunnel registration.
	wsURL := "ws" + server.URL[len("http"):] + "/register?name=devicetest"
	conn, wsResp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Auth-Token": {result.Token}})
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, wsResp.StatusCode)
	conn.Close()

}

func TestDeviceAuthorizeUnknownCode(t *testing.T) {
	guardian, _ := startFakeGuardianWithLogin(t)
	server := newDeviceTestServer(t, guardian.URL)

	resp, err := http.Get(server.URL + "/device/authorize?code=ZZZZ-9999")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDevicePollUnknownCode(t *testing.T) {
	guardian, _ := startFakeGuardianWithLogin(t)
	server := newDeviceTestServer(t, guardian.URL)

	resp, err := http.Post(server.URL+"/api/device/poll", "application/json", strings.NewReader(`{"device_code":"bogus"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCallbackRejectsForgedNonce(t *testing.T) {
	guardian, mintValid := startFakeGuardianWithLogin(t)
	server := newDeviceTestServer(t, guardian.URL)

	// Start a device auth to get a valid user code
	resp, err := http.Post(server.URL+"/api/device/start", "application/json", nil)
	require.NoError(t, err)
	var start struct {
		UserCode   string `json:"user_code"`
		DeviceCode string `json:"device_code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&start))
	resp.Body.Close()

	// Valid Guardian token, but a forged nonce in state: must not claim.
	cbURL := server.URL + "/auth/callback?token=" + url.QueryEscape(mintValid()) +
		"&state=" + url.QueryEscape("device:"+start.UserCode+":forged-nonce")
	resp, err = http.Get(cbURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// The pending auth must still be unclaimed.
	pollResp, err := http.Post(server.URL+"/api/device/poll", "application/json",
		strings.NewReader(`{"device_code":"`+start.DeviceCode+`"}`))
	require.NoError(t, err)
	defer pollResp.Body.Close()
	assert.Equal(t, http.StatusAccepted, pollResp.StatusCode, "auth should still be pending")
}

func TestTokenExchange(t *testing.T) {
	guardian, mintValid := startFakeGuardianWithLogin(t)
	server := newDeviceTestServer(t, guardian.URL)

	req, _ := http.NewRequest("POST", server.URL+"/api/token/exchange", nil)
	req.Header.Set("X-Auth-Token", mintValid())
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Token   string `json:"token"`
		User    string `json:"user"`
		Expires string `json:"expires"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.True(t, tunneltoken.IsTunnelToken(out.Token))
	assert.Equal(t, "ada@example.com", out.User)

	// Exchanged token works on auth-test
	req2, _ := http.NewRequest("GET", server.URL+"/api/auth-test", nil)
	req2.Header.Set("X-Auth-Token", out.Token)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var at map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&at))
	assert.Equal(t, "tunnel", at["auth_method"])
}
