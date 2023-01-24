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
	ips := append(r.Header["X-Forwarded-For"], r.RemoteAddr[:strings.LastIndex(r.RemoteAddr, ":")])
	for _, ip := range ips {
		// trim brackets from IPv6 addresses
		ip = strings.Trim(ip, "[]")
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

// request.Host contains the port which we don't want
// localhost:8000 -> localhost
//
//	shopify.com -> shopify.com
func getHostname(r *http.Request) string {
	if strings.Contains(r.Host, ":") {
		return r.Host[:strings.LastIndex(r.Host, ":")]
	}
	return r.Host
}

func isSubdomain(host, root string) bool {
	return strings.HasSuffix(host, root) && host != root
}

func GetSubdomain(r *http.Request, root string) (string, bool) {
	host := getHostname(r)
	if !isSubdomain(host, root) {
		return "", false
	}
	if host == root {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimSuffix(host, root), "."), true
}
