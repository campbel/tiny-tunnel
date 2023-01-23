package util

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
)

func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func JSS(v any) string {
	return string(JS(v))
}

func JS(v any) []byte {
	d, e := json.Marshal(v)
	Must(e)
	return d
}

func ErrString(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

func Must(err error) {
	if err == io.EOF {
		os.Exit(2)
		return
	}
	if err != nil {
		fmt.Println(err)
		os.Exit(3)
	}
}

func MustRead(r io.Reader) []byte {
	data, err := io.ReadAll(r)
	Must(err)
	return data
}

func WaitSigInt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = randomRune('0', 'z')
	}
	return string(b)
}

func randomRune(first, last rune) rune {
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(last-first+1)))
	return rune(v.Int64()) + first
}

func AllowedIP(r *http.Request, allowedIPs []string) bool {
	if len(allowedIPs) == 0 {
		return true
	}
	ips := append(r.Header["X-Forwarded-For"], strings.Split(r.RemoteAddr, ":")[0])
	for _, ip := range ips {
		pip := net.ParseIP(ip)
		if pip == nil {
			continue
		}
		for _, allowedIP := range allowedIPs {
			_, n, err := net.ParseCIDR(allowedIP)
			if err != nil {
				continue
			}
			if n.Contains(pip) {
				return true
			}
		}
	}
	return false
}
