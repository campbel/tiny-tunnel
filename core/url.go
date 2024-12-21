package core

import (
	"fmt"
	"net/url"
)

func getWebsocketURL(httpURL string) (*url.URL, error) {
	u, err := url.Parse(httpURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return nil, fmt.Errorf("invalid scheme: %s", u.Scheme)
	}
	return u, nil
}
