package http

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
)

var httpClient = func() http.Client {
	return http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxConnsPerHost:     100,
			IdleConnTimeout:     10 * time.Second,
			TLSHandshakeTimeout: 3 * time.Second,
		},
	}
}()

func Do(target string, req types.Request) types.Response {
	request, err := http.NewRequest(req.Method, target+req.Path, bytes.NewBuffer(req.Body))
	util.Must(err)
	request.Header = req.Headers
	response, err := httpClient.Do(request)
	if err != nil {
		return types.Response{Error: err.Error()}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return types.Response{
		ID:      req.ID,
		Status:  response.StatusCode,
		Headers: response.Header,
		Body:    body,
		Error:   util.ErrString(err),
	}
}
