package main

import (
	"fmt"

	"github.com/pion/webrtc/v3"
)

func main() {
	mediaEngine := new(webrtc.MediaEngine)
	mediaEngine.RegisterDefaultCodecs()

	webrtcAPI := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	webrtcAPI.NewPeerConnection(config)
	fmt.Println("Hello World")

}
