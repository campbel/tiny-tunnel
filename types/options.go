package types

import (
	"fmt"
	"net"
)

type ServerOptions struct {
	Port string `opts:"-p,--port" desc:"server port" default:"8000"`
}

type EchoOptions struct {
	Port string `opts:"-p,--port" desc:"server port" default:"7000"`
}

type ClientOptions struct {
	Target     string            `opts:"[0]"              desc:"target to proxy"`
	Name       string            `opts:"-n,--name"        desc:"name of the tunnel"`
	ServerHost string            `opts:"-s,--server-host" desc:"server hostname"                  default:"tt.campbel.io"`
	ServerPort string            `opts:"-p,--server-port" desc:"server port"                      default:"443"`
	Insecure   bool              `opts:"-k,--insecure"    desc:"use insecure HTTP and WebSockets" default:"false"`
	AllowIPs   []string          `opts:"-a,--allow-ip"    desc:"IP CIDR ranges to allow"          default:"0.0.0.0/0,::/0"`
	Headers    map[string]string `opts:"-h,--header"      desc:"headers to add to requests"`
}

func (c ClientOptions) SchemeHTTP() string {
	if c.Insecure {
		return "http"
	}
	return "https"
}

func (c ClientOptions) SchemeWS() string {
	if c.Insecure {
		return "ws"
	}
	return "wss"
}

func (c ClientOptions) Valid() error {
	for _, ip := range c.AllowIPs {
		if _, _, err := net.ParseCIDR(ip); err != nil {
			return fmt.Errorf("invalid IP CIDR range specified: %s", ip)
		}
	}
	return nil
}
