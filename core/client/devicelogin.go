package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DeviceLoginResult is the outcome of a completed device authorization.
type DeviceLoginResult struct {
	Token   string
	User    string
	Expires string
}

// DeviceLogin runs the device authorization flow against a tunnel server:
// requests a user code, surfaces it via prompt, and polls until the user
// approves in a browser elsewhere. Intended for headless environments.
func DeviceLogin(ctx context.Context, serverURL string, prompt func(verificationURI, verificationURIComplete, userCode string)) (*DeviceLoginResult, error) {
	base := strings.TrimSuffix(serverURL, "/")
	httpClient := &http.Client{Timeout: 15 * time.Second}

	// Start
	resp, err := httpClient.Post(base+"/api/device/start", "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("device start: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("this server does not support device login (older version?)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device start: unexpected status %d", resp.StatusCode)
	}

	var start struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&start); err != nil {
		return nil, fmt.Errorf("device start: %w", err)
	}
	if start.DeviceCode == "" || start.UserCode == "" {
		return nil, errors.New("device start: incomplete response")
	}

	if prompt != nil {
		prompt(start.VerificationURI, start.VerificationURIComplete, start.UserCode)
	}

	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)

	// Poll
	body, _ := json.Marshal(map[string]string{"device_code": start.DeviceCode})
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return nil, errors.New("device login expired before approval — run tnl login --device again")
		}

		resp, err := httpClient.Post(base+"/api/device/poll", "application/json", bytes.NewReader(body))
		if err != nil {
			// transient network error: keep polling
			continue
		}
		var out struct {
			Status  string `json:"status"`
			Token   string `json:"token"`
			User    string `json:"user"`
			Expires string `json:"expires"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if decodeErr != nil {
			continue
		}

		switch out.Status {
		case "ok":
			return &DeviceLoginResult{Token: out.Token, User: out.User, Expires: out.Expires}, nil
		case "pending":
			continue
		case "expired":
			return nil, errors.New("device login expired or was already used — run tnl login --device again")
		default:
			continue
		}
	}
}

// ExchangeToken swaps a Guardian credential for a long-lived tnl tunnel
// token via the server's exchange endpoint. Returns ("", nil) if the server
// doesn't support exchange (older version) so callers can fall back to the
// Guardian token itself.
func ExchangeToken(ctx context.Context, serverURL, guardianCredential string) (token string, expires string, err error) {
	base := strings.TrimSuffix(serverURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/token/exchange", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("X-Auth-Token", guardianCredential)

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return "", "", nil // older server: no exchange endpoint
	case http.StatusUnauthorized:
		return "", "", errors.New("exchange rejected: credential invalid")
	default:
		return "", "", fmt.Errorf("exchange failed: unexpected status %d", resp.StatusCode)
	}

	var out struct {
		Token   string `json:"token"`
		Expires string `json:"expires"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Token, out.Expires, nil
}
