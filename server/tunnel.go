package server

import (
	"fmt"
	"net/http"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"

	"github.com/gorilla/websocket"
)

type Tunnel struct {
	ID          string
	sendChannel chan (types.Message)
	AllowedIPs  []string

	WSSessions *sync.Map[string, *sync.WSConn]
	Responses  *sync.Map[string, chan (types.Message)]
}

func NewTunnel(id string, allowedIPs []string) *Tunnel {
	return &Tunnel{
		ID:          id,
		sendChannel: make(chan (types.Message)),
		AllowedIPs:  allowedIPs,
		WSSessions:  sync.NewMap[string, *sync.WSConn](),
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

func (t *Tunnel) Run(w http.ResponseWriter, r *http.Request) error {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer rawConn.Close()

	conn := sync.NewWSConn(rawConn)

	// Channel to sync the reader and writer
	done := make(chan bool)

	// Read from the websocket connection
	go func() {
		defer func() {
			done <- true
		}()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			message := types.LoadMessage(data)

			// Handle messages with expected responses
			if message.ResponseTo != "" {
				if responseChan, ok := t.Responses.Get(message.ResponseTo); !ok {
					log.Error("response undeliverable", "re", message.ResponseTo, "tunnel", t.ID)
				} else {
					responseChan <- message
					t.Responses.Delete(message.ResponseTo)
				}
				continue
			}

			// Handle websocket messages
			if message.Kind == types.MessageKindWebsocketMessage {
				wsMessage := types.LoadWebsocketMessage(message.Payload)
				wsConn, ok := t.WSSessions.Get(wsMessage.SessionID)
				if !ok {
					log.Info("failed to get websocket connection", "session", wsMessage.SessionID)
					continue
				}

				if wsMessage.IsBinary() {
					if err := wsConn.WriteMessage(websocket.BinaryMessage, wsMessage.BinaryData); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
						continue
					}
				} else {
					if err := wsConn.WriteMessage(websocket.TextMessage, []byte(wsMessage.StringData)); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
						continue
					}
				}
				log.Info("websocket message sent successfully", "session", wsMessage.SessionID, "data", string(wsMessage.BinaryData))
			}
		}
	}()

	// Write messages
LOOP:
	for {
		select {
		case <-done:
			break LOOP
		case msg := <-t.sendChannel:
			if err := conn.WriteMessage(websocket.BinaryMessage, msg.JSON()); err != nil {
				log.Info("error writing request", "err", err, "tunnel", t.ID)
				break LOOP
			}
		}
	}

	log.Info("closing writes", "tunnel", t.ID)
	return nil
}
