package core

import (
	"fmt"
	"net"
	"net/http"
)

type ClientOptions struct {
	Target            string
	Name              string
	ServerHost        string
	ServerPort        string
	Insecure          bool
	AllowedIPs        []string
	ReconnectAttempts int
	TargetHeaders     http.Header
	ServerHeaders     http.Header
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
