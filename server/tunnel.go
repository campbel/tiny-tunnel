package server

import (
	"fmt"

	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"

	gws "github.com/gorilla/websocket"
)

type Tunnel struct {
	ID          string
	sendChannel chan (types.Message)
	AllowedIPs  []string

	WSSessions *sync.Map[string, *gws.Conn]
	Responses  *sync.Map[string, chan (types.Message)]
}

func NewTunnel(id string, allowedIPs []string) *Tunnel {
	return &Tunnel{
		ID:          id,
		sendChannel: make(chan (types.Message)),
		AllowedIPs:  allowedIPs,
		WSSessions:  sync.NewMap[string, *gws.Conn](),
		Responses:   sync.NewMap[string, chan (types.Message)](),
	}
}

func (t *Tunnel) Send(kind string, payload []byte, responseChan chan (types.Message)) error {
	messageID := util.RandString(24)
	if responseChan != nil {
		if !t.Responses.SetNX(messageID, responseChan) {
			return fmt.Errorf("failed to set response channel")
		}
	}
	t.sendChannel <- types.Message{
		ID:      messageID,
		Kind:    kind,
		Payload: payload,
	}
	return nil
}
