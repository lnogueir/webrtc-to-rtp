package wshandles

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pion/webrtc/v3"
)

var mediaEngine *webrtc.MediaEngine
var webrtcAPI *webrtc.API

func init() {
	mediaEngine = new(webrtc.MediaEngine)
	mediaEngine.RegisterDefaultCodecs()
	webrtcAPI = webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	log.Print("[webrtc] Initialized")
}

type webrtcState int

const (
	notStarted webrtcState = iota
	readyToBegin
	signalling
	streaming
)

func (s webrtcState) String() string {
	return [...]string{"NotStarted", "ReadyToBegin", "Signalling", "Streaming"}[s]
}

type webrtcMessage struct {
	Command   string `json:"cmd,required"`
	StreamID  string `json:"streamId,required"`
	Sdp       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

//WebRTCHandle handles webrtc related requests
func WebRTCHandle(w http.ResponseWriter, r *http.Request) {
	handleState := notStarted
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if nil != err {
		log.Println("[webrtc] Error connecting:", err)
		return
	}
	defer conn.Close()

	var errMessage string
	for {
		mt, message, err := conn.ReadMessage()
		if nil != err {
			errMessage = "[webrtc] Error reading message:"
			log.Println(errMessage, err)
			conn.WriteMessage(mt, []byte(errMessage))
			continue
		}
		log.Printf("[webrtc] Recv message: %s", message)

		var parsedMessage webrtcMessage
		err = json.Unmarshal([]byte(message), &parsedMessage)
		if nil != err {
			errMessage = "[webrtc] Error parsing message:"
			log.Println(errMessage, err)
			continue
		}

		switch parsedMessage.Command {
		case "start":
			if notStarted != handleState {
				errMessage = "[webrtc] Called '%s' cmd in wrong state: %s"
				log.Printf(errMessage, parsedMessage.Command, handleState)
				break
			}
			// webrtcAPI.NewPeerConnection()
			handleState = readyToBegin

		}

		// err = conn.WriteMessage(mt, message)
		// if err != nil {
		// 	log.Println("write:", err)
		// 	break
		// }
	}

	// config := webrtc.Configuration{
	// 	ICEServers: []webrtc.ICEServer{
	// 		{
	// 			URLs: []string{"stun:stun.l.google.com:19302"},
	// 		},
	// 	},
	// }
	// webrtcAPI.NewPeerConnection(config)
}
