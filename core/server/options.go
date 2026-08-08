package server

import (
	"fmt"
)

type Options struct {
	Hostname     string
	AccessScheme string
	AccessPort   string
	EnableAuth   bool
	// GuardianURL is the Guardian base URL / issuer used to verify
	// credentials when EnableAuth is true (e.g. https://id.stable.dexus.io).
	GuardianURL string
	// GuardianAudience is the Guardian service client ID expected in JWT
	// aud claims (e.g. svc_tiny-tunnel_stable). Empty skips the aud check.
	GuardianAudience string
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
