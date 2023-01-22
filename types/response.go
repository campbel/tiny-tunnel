package types

import (
	"encoding/json"
	"net/http"

	"github.com/campbel/tiny-tunnel/util"
)

type Response struct {
	Status  int
	Headers http.Header
	Body    []byte
	Error   string
}

func (r Response) JSON() []byte {
	return util.JS(r)
}

func LoadResponse(data []byte) Response {
	var resp Response
	util.Must(json.Unmarshal(data, &resp))
	return resp
}
