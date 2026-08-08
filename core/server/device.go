package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Device authorization flow (RFC 8628-style) for headless environments.
//
// A CLI without a browser (VAPE pod, SSH box, CI) calls /api/device/start,
// shows the user an 8-char code plus https://<server>/device, and polls
// /api/device/poll. The user opens that page on any machine with a browser,
// confirms the code, and runs the normal Guardian SSO using the server's
// primary registered redirect URI (/auth/callback — exact match, no PAR).
// The server verifies the Guardian identity, mints a long-lived tnl token,
// and hands it to the waiting CLI.

const (
	deviceCodeTTL      = 10 * time.Minute
	devicePollInterval = 3                                  // seconds, hint for clients
	userCodeAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
)

type deviceAuth struct {
	userCode   string
	deviceCode string
	nonce      string // binds the SSO callback to this pending auth
	expiresAt  time.Time

	claimed bool
	token   string
	expires time.Time
	user    string
}

type deviceStore struct {
	mu         sync.Mutex
	byUserCode map[string]*deviceAuth
}

func newDeviceStore() *deviceStore {
	return &deviceStore{byUserCode: map[string]*deviceAuth{}}
}

func (s *deviceStore) create() (*deviceAuth, error) {
	userCode, err := randomUserCode()
	if err != nil {
		return nil, err
	}
	deviceCode, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	nonce, err := randomToken(16)
	if err != nil {
		return nil, err
	}

	auth := &deviceAuth{
		userCode:   userCode,
		deviceCode: deviceCode,
		nonce:      nonce,
		expiresAt:  time.Now().Add(deviceCodeTTL),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if _, exists := s.byUserCode[userCode]; exists {
		return nil, errors.New("user code collision, retry")
	}
	s.byUserCode[userCode] = auth
	return auth, nil
}

func (s *deviceStore) getByUserCode(userCode string) (*deviceAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	auth, ok := s.byUserCode[normalizeUserCode(userCode)]
	return auth, ok
}

// claim binds a minted token to a pending auth, verified by nonce.
func (s *deviceStore) claim(userCode, nonce, token, user string, expires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	auth, ok := s.byUserCode[normalizeUserCode(userCode)]
	if !ok || time.Now().After(auth.expiresAt) {
		return errors.New("code expired or unknown")
	}
	if subtle.ConstantTimeCompare([]byte(auth.nonce), []byte(nonce)) != 1 {
		return errors.New("nonce mismatch")
	}
	if auth.claimed {
		return errors.New("code already used")
	}
	auth.claimed = true
	auth.token = token
	auth.user = user
	auth.expires = expires
	return nil
}

// poll returns (token, expires, user, done) for a device code. Single-use:
// a successful poll removes the record.
func (s *deviceStore) poll(deviceCode string) (token string, expires time.Time, user string, done bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	for _, auth := range s.byUserCode {
		if subtle.ConstantTimeCompare([]byte(auth.deviceCode), []byte(deviceCode)) == 1 {
			if !auth.claimed {
				return "", time.Time{}, "", false, nil
			}
			delete(s.byUserCode, auth.userCode)
			return auth.token, auth.expires, auth.user, true, nil
		}
	}
	return "", time.Time{}, "", false, errors.New("expired_token")
}

func (s *deviceStore) sweepLocked() {
	now := time.Now()
	for code, auth := range s.byUserCode {
		// Claimed-but-unpolled entries keep living until expiry so the CLI
		// can pick them up; everything else dies at expiresAt.
		if now.After(auth.expiresAt) {
			delete(s.byUserCode, code)
		}
	}
}

func randomUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, 9)
	for i := 0; i < 8; i++ {
		pos := i
		if i >= 4 {
			pos = i + 1
		}
		code[pos] = userCodeAlphabet[int(b[i])%len(userCodeAlphabet)]
	}
	code[4] = '-'
	return string(code), nil
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	if len(code) == 8 && !strings.Contains(code, "-") {
		code = code[:4] + "-" + code[4:]
	}
	return code
}

// serverBaseURL is the public URL of this tunnel server (apex host).
func (s *Handler) serverBaseURL() string {
	return fmt.Sprintf("%s://%s%s", s.options.GetAccessScheme(), s.options.Hostname, s.options.GetAccessPort())
}

// --- HTTP handlers ---

// HandleDeviceStart begins a device authorization: POST /api/device/start
func (s *Handler) HandleDeviceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth, err := s.devices.create()
	if err != nil {
		s.l.Error("failed to create device auth", "err", err.Error())
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	verificationURI := s.serverBaseURL() + "/device"
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"device_code":               auth.deviceCode,
		"user_code":                 auth.userCode,
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURI + "?code=" + url.QueryEscape(auth.userCode),
		"expires_in":                int(time.Until(auth.expiresAt).Seconds()),
		"interval":                  devicePollInterval,
	})
}

// HandleDevicePoll returns the vended token once the user approved:
// POST /api/device/poll {"device_code": "..."}
func (s *Handler) HandleDevicePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceCode == "" {
		http.Error(w, "device_code required", http.StatusBadRequest)
		return
	}

	token, expires, user, done, err := s.devices.poll(body.DeviceCode)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"status": "expired"})
		return
	}
	if !done {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{"status": "pending", "interval": devicePollInterval})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"token":   token,
		"user":    user,
		"expires": expires.Format(time.RFC3339),
	})
}

// HandleDevicePage renders the code-confirmation page: GET /device[?code=...]
func (s *Handler) HandleDevicePage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-TT-Tunnel") != "" {
		s.HandleTunnelRequest(w, r)
		return
	}
	code := normalizeUserCode(r.URL.Query().Get("code"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, devicePageHTML, html.EscapeString(code))
}

// HandleDeviceAuthorize validates the code and bounces through Guardian SSO:
// GET /device/authorize?code=XXXX-XXXX
func (s *Handler) HandleDeviceAuthorize(w http.ResponseWriter, r *http.Request) {
	code := normalizeUserCode(r.URL.Query().Get("code"))
	auth, ok := s.devices.getByUserCode(code)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, deviceErrorHTML("Unknown or expired code. Re-run <code>tnl login --device</code> and try again."))
		return
	}

	// State round-trips through Guardian back to /auth/callback.
	state := "device:" + auth.userCode + ":" + auth.nonce
	loginURL := fmt.Sprintf("%s/auth/login?client_id=%s&redirect_uri=%s&state=%s",
		strings.TrimSuffix(s.options.GuardianURL, "/"),
		url.QueryEscape(s.options.GuardianAudience),
		url.QueryEscape(s.serverBaseURL()+"/auth/callback"),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, loginURL, http.StatusFound)
}

// HandleAuthCallback receives the Guardian token flow redirect:
// GET /auth/callback?token=...&state=device:<user_code>:<nonce>
func (s *Handler) HandleAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	token := q.Get("token")
	state := q.Get("state")
	if token == "" || !strings.HasPrefix(state, "device:") {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, deviceErrorHTML("Missing token or unexpected state. Start over from <code>tnl login --device</code>."))
		return
	}
	parts := strings.SplitN(state, ":", 3)
	if len(parts) != 3 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, deviceErrorHTML("Malformed state."))
		return
	}
	userCode, nonce := parts[1], parts[2]

	// Verify the Guardian credential and mint a tnl token for that identity.
	identity, err := s.verifier.Verify(r.Context(), token)
	if err != nil {
		s.l.Info("device flow: rejected guardian credential", "err", err.Error())
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, deviceErrorHTML("Guardian sign-in could not be verified."))
		return
	}

	minted, expires, err := s.signer.Mint(identity.Sub, identity.Email)
	if err != nil {
		s.l.Error("device flow: mint failed", "err", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, deviceErrorHTML("Failed to mint a tunnel token."))
		return
	}

	if err := s.devices.claim(userCode, nonce, minted, identity.String(), expires); err != nil {
		s.l.Info("device flow: claim failed", "err", err.Error(), "user_code", userCode)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, deviceErrorHTML("This code has expired or was already used. Re-run <code>tnl login --device</code>."))
		return
	}

	s.l.Info("device flow: token vended", "user", identity.String(), "user_code", userCode)
	fmt.Fprintf(w, deviceSuccessHTML, html.EscapeString(identity.String()))
}

// HandleTokenExchange swaps a verified Guardian credential for a long-lived
// tnl token: POST /api/token/exchange (behind authTokenMiddleware).
func (s *Handler) HandleTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	identity, ok := identityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	minted, expires, err := s.signer.Mint(identity.Sub, identity.Email)
	if err != nil {
		s.l.Error("token exchange: mint failed", "err", err.Error())
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	s.l.Info("token exchange: tunnel token vended", "user", identity.String())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"token":   minted,
		"user":    identity.String(),
		"expires": expires.Format(time.RFC3339),
	})
}

// --- HTML ---

const devicePageHTML = `<!DOCTYPE html>
<html><head><title>Tiny Tunnel — Device Login</title><style>
body { font-family: -apple-system, sans-serif; display: flex; justify-content: center; padding-top: 6rem; background: #0d1117; color: #e6edf3; }
.card { text-align: center; max-width: 26rem; }
input { font-size: 1.6rem; text-align: center; letter-spacing: 0.3rem; width: 14rem; padding: 0.5rem; margin: 1.5rem 0; background: #161b22; color: #e6edf3; border: 1px solid #30363d; border-radius: 8px; text-transform: uppercase; }
button { font-size: 1rem; padding: 0.6rem 2rem; background: #238636; color: white; border: 0; border-radius: 8px; cursor: pointer; }
</style></head><body><div class="card">
<h2>Device login</h2>
<p>Enter the code shown in your terminal, then continue with SSO.</p>
<form action="/device/authorize" method="get">
<input name="code" value="%s" placeholder="XXXX-XXXX" autofocus autocomplete="off" spellcheck="false">
<br><button type="submit">Continue with SSO</button>
</form>
</div></body></html>`

const deviceSuccessHTML = `<!DOCTYPE html>
<html><head><title>Tiny Tunnel — Success</title><style>
body { font-family: -apple-system, sans-serif; display: flex; justify-content: center; padding-top: 6rem; background: #0d1117; color: #e6edf3; text-align: center; }
</style></head><body><div>
<h2>&#9989; Device authorized</h2>
<p>Signed in as <strong>%s</strong>.</p>
<p>You can close this tab — the terminal will pick up the token within a few seconds.</p>
</div></body></html>`

func deviceErrorHTML(msg string) string {
	return `<!DOCTYPE html>
<html><head><title>Tiny Tunnel — Error</title><style>
body { font-family: -apple-system, sans-serif; display: flex; justify-content: center; padding-top: 6rem; background: #0d1117; color: #e6edf3; text-align: center; }
</style></head><body><div>
<h2>&#10060; Something went wrong</h2>
<p>` + msg + `</p>
</div></body></html>`
}
