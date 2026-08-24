package server

import (
	"fmt"
	"time"
)

type Options struct {
	Hostname     string
	AccessScheme string
	AccessPort   string
	EnableAuth   bool
	// PublicHostname is an optional second hostname that tunnel-proxy traffic
	// (the wildcard {tunnel}.<hostname> route) is additionally served on,
	// alongside Hostname. Management endpoints (/register, /device, the UI,
	// etc.) are NOT duplicated onto it -- only HandleTunnelRequest. This lets
	// an internal-only Hostname (e.g. tnl.stable.dexus.io, reachable only via
	// Guardian/WARP) coexist with a public PublicHostname (e.g.
	// tnl.dutchie.dev) that external services can reach, without exposing the
	// management API on the public hostname too. Empty disables the second
	// route entirely.
	PublicHostname string
	// GuardianURL is the Guardian base URL / issuer used to verify
	// credentials when EnableAuth is true (e.g. https://id.stable.dexus.io).
	GuardianURL string
	// GuardianAudience is the Guardian service client ID expected in JWT
	// aud claims (e.g. svc_tiny-tunnel_stable). Empty skips the aud check.
	GuardianAudience string
	// SigningKey is a base64-encoded Ed25519 seed used to mint tnl's own
	// long-lived tunnel tokens (TINY_TUNNEL_SIGNING_KEY). When empty an
	// ephemeral key is generated: tokens then die with the process.
	SigningKey string
	// TokenTTL is the lifetime of vended tunnel tokens (default 30 days).
	TokenTTL time.Duration
}

func (o Options) GetTunnelURL(name string) string {
	return fmt.Sprintf("%s://%s.%s%s", o.GetAccessScheme(), name, o.Hostname, o.GetAccessPort())
}

func (o Options) GetAccessScheme() string {
	if o.AccessScheme == "" {
		return "https"
	}
	return o.AccessScheme
}

func (o Options) GetAccessPort() string {
	if o.AccessPort == "" {
		return ""
	}
	return ":" + o.AccessPort
}
