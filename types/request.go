package types

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/util"
)

type Request struct {
	Method       string          `json:"method,omitempty"`
	Path         string          `json:"path,omitempty"`
	Headers      http.Header     `json:"headers,omitempty"`
	Body         []byte          `json:"body,omitempty"`
	CreatedAt    time.Time       `json:"created_at,omitempty"`
	ResponseChan chan (Response) `json:"-"`
}

func (r Request) JSON() []byte {
	return util.JS(r)
}

func LoadRequest(data []byte) Request {
	var req Request
	util.Must(json.Unmarshal(data, &req))
	return req
}
