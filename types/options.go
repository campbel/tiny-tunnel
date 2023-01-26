package types

import (
	"fmt"
	"net"
)

type YoshiApp struct {
	Options GlobalOptions
	Server  struct {
		Options ServerOptions
		Run     func(ServerOptions)
	}
	Echo struct {
		Options EchoOptions
		Run     func(EchoOptions)
	}
	Client struct {
		Options ClientOptions
		Run     func(ClientOptions)
	}
}

type GlobalOptions struct {
	Verbose bool `yoshi-flag:"-v,--verbose" yoshi-desc:"verbose logging"`
}

type ServerOptions struct {
	Port     string `yoshi-flag:"-p,--port" yoshi-desc:"server port" yoshi-def:"8000"`
	Hostname string `yoshi-flag:"-h,--hostname" yoshi-desc:"server hostname" yoshi-def:"localhost"`
}

type EchoOptions struct {
	Port string `yoshi-flag:"-p,--port" yoshi-desc:"server port" yoshi-def:"7000"`
}

type ClientOptions struct {
	Target            string            `json:"target"      yoshi-flag:"-t,--target"        yoshi-desc:"target to proxy"`
	Name              string            `json:"name"        yoshi-flag:"-n,--name"          yoshi-desc:"name of the tunnel"`
	ServerHost        string            `json:"server-host" yoshi-flag:"-s,--server-host"   yoshi-desc:"server hostname"                  yoshi-def:"localhost"`
	ServerPort        string            `json:"server-port" yoshi-flag:"-p,--server-port"   yoshi-desc:"server port"                      yoshi-def:"8000"`
	Insecure          bool              `json:"insecure"    yoshi-flag:"-k,--insecure"      yoshi-desc:"use insecure HTTP and WebSockets" yoshi-def:"true"`
	AllowedIPs        []string          `json:"allowed-ips" yoshi-flag:"-a,--allow-ip"      yoshi-desc:"IP CIDR ranges to allow"          yoshi-def:"0.0.0.0/0,::/0"`
	ReconnectAttempts int               `json:"-"           yoshi-flag:"-r,--max-reconnect" yoshi-desc:"max reconnect attempts"           yoshi-def:"5"`
	Headers           map[string]string `json:"headers"     yoshi-flag:"-h,--header"        yoshi-desc:"headers to add to requests"`
}

func (c ClientOptions) Origin() string {
	return c.SchemeHTTP() + "://" + c.ServerHost
}

func (c ClientOptions) URL() string {
	return c.SchemeWS() + "://" + c.ServerHost + ":" + c.ServerPort + "/register?name=" + c.Name
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
	var errs []error
	if c.Name == "" {
		errs = append(errs, fmt.Errorf("name is required"))
	}
	if c.Target == "" {
		errs = append(errs, fmt.Errorf("target is required"))
	}
	for _, ip := range c.AllowedIPs {
		if _, _, err := net.ParseCIDR(ip); err != nil {
			errs = append(errs, fmt.Errorf("invalid IP CIDR range specified: %s", ip))
		}
	}
	var finalErr error
	for _, err := range errs {
		if finalErr == nil {
			finalErr = err
		} else {
			finalErr = fmt.Errorf("%w\n%s", finalErr, err)
		}
	}
	return finalErr
}
