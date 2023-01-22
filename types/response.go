package types

import (
	"encoding/json"
	"net/http"

	"github.com/campbel/tiny-tunnel/util"
)

type Response struct {
	ID      string      `json:"id,omitempty"`
	Status  int         `json:"status,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Body    []byte      `json:"body,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func (r Response) JSON() []byte {
	return util.JS(r)
}

func LoadResponse(data []byte) Response {
	var resp Response
	util.Must(json.Unmarshal(data, &resp))
	return resp
}
