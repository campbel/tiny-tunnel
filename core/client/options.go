package client

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
)

type Options struct {
	Target            string
	Name              string
	ServerHost        string
	ServerPort        string
	Insecure          bool
	AllowedIPs        []string
	ReconnectAttempts int
	TargetHeaders     http.Header
	ServerHeaders     http.Header

	OutputWriter io.Writer
}

func (c Options) Origin() string {
	return c.SchemeHTTP() + "://" + c.ServerHost
}

func (c Options) URL() string {
	return c.SchemeWS() + "://" + c.ServerHost + ":" + c.ServerPort + "/register?name=" + c.Name
}

func (c Options) SchemeHTTP() string {
	if c.Insecure {
		return "http"
	}
	return "https"
}

func (c Options) SchemeWS() string {
	if c.Insecure {
		return "ws"
	}
	return "wss"
}

func (c Options) Output() io.Writer {
	if c.OutputWriter == nil {
		return os.Stdout
	}
	return c.OutputWriter
}

func (c Options) Valid() error {
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
