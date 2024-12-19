package types

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/util"
)

type HTTPRequest struct {
	Method    string      `json:"method,omitempty"`
	Path      string      `json:"path,omitempty"`
	Headers   http.Header `json:"headers,omitempty"`
	Body      []byte      `json:"body,omitempty"`
	CreatedAt time.Time   `json:"created_at,omitempty"`
}

func (r HTTPRequest) JSON() []byte {
	return util.JS(r)
}

func LoadRequest(data []byte) HTTPRequest {
	var req HTTPRequest
	util.Must(json.Unmarshal(data, &req))
	return req
}

type HTTPResponse struct {
	Status  int         `json:"status,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Body    []byte      `json:"body,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func (r HTTPResponse) JSON() []byte {
	return util.JS(r)
}

func LoadResponse(data []byte) HTTPResponse {
	var resp HTTPResponse
	util.Must(json.Unmarshal(data, &resp))
	return resp
}
