package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/campbel/tiny-tunnel/types"
	"github.com/stretchr/testify/assert"
)

func TestDo(t *testing.T) {
	assert := assert.New(t)

	req := types.Request{
		Method:  "GET",
		Path:    "/",
		Body:    []byte{},
		Headers: http.Header{},
		ID:      "123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	}))

	response := Do(server.URL, req)

	assert.Equal("123", response.ID)
	assert.Equal(http.StatusOK, response.Status)
	assert.Equal("Hello, World!", string(response.Body))
}
