package server

import (
	"fmt"
	"os"
	"time"
)

type Options struct {
	Hostname    string
	EnableAuth  bool
	TokenExpiry time.Duration
}

func (o Options) GetJWTSecret() string {
	return os.Getenv("TINY_TUNNEL_JWT_SECRET")
}

func (o Options) GetTokenExpiry() time.Duration {
	expiry, ok := os.LookupEnv("TINY_TUNNEL_TOKEN_EXPIRY")
	if !ok {
		return 30 * 24 * time.Hour
	}

	d, err := time.ParseDuration(expiry)
	if err != nil {
		panic(fmt.Sprintf("invalid token expiry: %s", err))
	}

	return d
}
