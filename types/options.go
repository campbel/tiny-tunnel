package types

import (
	"fmt"
	"net"
)

type YoshiApp struct {
	Server func(ServerOptions)
	Echo   func(EchoOptions)
	Client func(ClientOptions)
}

type GlobalOptions struct {
	Verbose bool `yoshi:"-v,--verbose" yoshi-description:"verbose logging"`
}

type ServerOptions struct {
	Port        string `json:"port"     yoshi:"-p,--port"     yoshi-description:"server port"     yoshi-default:"8000"`
	Hostname    string `json:"hostname" yoshi:"-h,--hostname" yoshi-description:"server hostname" yoshi-default:"localhost"`
	LetsEncrypt bool   `json:"encrypt"  yoshi:"-l,--encrypt"`
}

type EchoOptions struct {
	Port string `yoshi:"-p,--port" yoshi-description:"server port" yoshi-default:"7000"`
}

type ClientOptions struct {
	Target            string            `json:"target"         yoshi:"-t,--target"        yoshi-description:"target to proxy"`
	Name              string            `json:"name"           yoshi:"-n,--name"          yoshi-description:"name of the tunnel"`
	ServerHost        string            `json:"server_host"    yoshi:"-s,--server-host"   yoshi-description:"server hostname"                  yoshi-default:"localhost"`
	ServerPort        string            `json:"server_port"    yoshi:"-p,--server-port"   yoshi-description:"server port"                      yoshi-default:"8000"`
	Insecure          bool              `json:"insecure"       yoshi:"-k,--insecure"      yoshi-description:"use insecure HTTP and WebSockets" yoshi-default:"true"`
	AllowedIPs        []string          `json:"allowed_ips"    yoshi:"-a,--allow-ip"      yoshi-description:"IP CIDR ranges to allow"          yoshi-default:"0.0.0.0/0,::/0"`
	ReconnectAttempts int               `json:"-"              yoshi:"-r,--max-reconnect" yoshi-description:"max reconnect attempts"           yoshi-default:"5"`
	TargetHeaders     map[string]string `json:"headers"        yoshi:"-h,--header"        yoshi-description:"headers to add to requests to target"`
	ServerHeaders     map[string]string `json:"-"              yoshi:"--server-header"    yoshi-description:"headers to add to requests to server"`
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
