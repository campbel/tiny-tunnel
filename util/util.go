package util

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"os"
	"os/signal"
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
		os.Exit(0)
		return
	}
	if err != nil {
		panic(err)
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
	v, err := rand.Int(rand.Reader, big.NewInt(int64(last-first)))
	if err != nil {
		log.Println(err)
	}
	return rune(v.Int64()) + first
}
