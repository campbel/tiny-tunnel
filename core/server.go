package core

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
)

type Server struct {
	tunnel *Tunnel
}

func NewServer(conn *websocket.Conn) *Server {
	tunnel := NewTunnel(conn)
	tunnel.Run()
	return &Server{tunnel: tunnel}
}

func (s *Server) HandleHttpRequest(w http.ResponseWriter, r *http.Request) {
	responseChannel := make(chan Message)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	s.tunnel.Send(MessageKindHttpRequest, &HttpRequestPayload{
		Method:  r.Method,
		URL:     r.URL.String(),
		Headers: r.Header,
		Body:    bodyBytes,
	}, responseChannel)

	response := <-responseChannel

	// If the response is not a HttpResponse, we need to return an error
	if response.Kind != MessageKindHttpResponse {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var responsePayload HttpResponsePayload
	if err := json.Unmarshal(response.Payload, &responsePayload); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	for k, v := range responsePayload.Response.Headers {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	w.WriteHeader(responsePayload.Response.Status)
	w.Write(responsePayload.Response.Body)
}

func (s *Server) HandleWebsocketCreateRequest(w http.ResponseWriter, r *http.Request) {

}
