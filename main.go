package main

import (
	"log"
	"net/http"

	"github.com/lnogueir/stream-anything/wshandles"
)

func main() {
	http.HandleFunc("/webrtc", wshandles.WebRTCHandle)
	const addr = "localhost:8080"
	log.Printf("WebSocket listening at %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
