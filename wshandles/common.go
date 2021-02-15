package wshandles

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// allow all connections
		return true
	},
}

type baseHandle struct {
	wsConn *websocket.Conn
	id     string
	mutex  sync.Mutex
}

func (handle *baseHandle) send(messageStruct interface{}) {
	message, _ := json.Marshal(messageStruct)
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	handle.wsConn.WriteMessage(websocket.TextMessage, message)
}

func (handle *baseHandle) sendWarning(text string) {
	handle.send(struct {
		Warning string `json:"warning"`
	}{text})
}

func (handle *baseHandle) sendError(text string) {
	handle.send(struct {
		Error string `json:"error"`
	}{text})
}

func (handle *baseHandle) sendStatus(text string) {
	handle.send(struct {
		Status string `json:"status"`
	}{text})
}
