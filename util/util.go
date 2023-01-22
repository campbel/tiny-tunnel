package util

import (
	"encoding/json"
	"io"
	"os"
	"os/signal"
)

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
