package client

import (
	"fmt"
	"net"
)

type ConnectOptions struct {
	Target            string
	Name              string
	ServerHost        string
	ServerPort        string
	Insecure          bool
	AllowedIPs        []string
	ReconnectAttempts int
	TargetHeaders     map[string]string
	ServerHeaders     map[string]string
}

func (c ConnectOptions) Origin() string {
	return c.SchemeHTTP() + "://" + c.ServerHost
}

func (c ConnectOptions) URL() string {
	return c.SchemeWS() + "://" + c.ServerHost + ":" + c.ServerPort + "/register?name=" + c.Name
}

func (c ConnectOptions) SchemeHTTP() string {
	if c.Insecure {
		return "http"
	}
	return "https"
}

func (c ConnectOptions) SchemeWS() string {
	if c.Insecure {
		return "ws"
	}
	return "wss"
}

func (c ConnectOptions) Valid() error {
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
