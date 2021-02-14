package wshandles

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
	failed
)

func (s webrtcState) String() string {
	return [...]string{"NotStarted", "ReadyToBegin", "Signalling", "Streaming", "Failed"}[s]
}

type webrtcMessage struct {
	Command       string  `json:"cmd,required"`
	Sdp           string  `json:"sdp,omitempty"`
	Candidate     string  `json:"candidate,omitempty"`
	SDPMid        string  `json:"sdpMid,omitempty"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`
}

type webrtcHandle struct {
	state    webrtcState
	wsConn   *websocket.Conn
	peerConn *webrtc.PeerConnection
	mutex    sync.Mutex
	id       string
}

//WebRTCHandle handles webrtc related requests
func WebRTCHandle(w http.ResponseWriter, r *http.Request) {
	var err error
	handle := webrtcHandle{
		id:    uuid.NewString(),
		state: notStarted,
	}
	if handle.wsConn, err = wsUpgrader.Upgrade(w, r, nil); nil != err {
		log.Println("[webrtc] Error connecting:", err)
		return
	}
	defer handle.wsConn.Close()

	var errMessage string
	for {
		_, message, err := handle.wsConn.ReadMessage()
		if nil != err {
			if !websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[webrtc] Terminating handle %s", handle.id)
				return
			}
			errMessage = "[webrtc] Error reading message:"
			log.Println(errMessage, err)
			continue
		}
		log.Printf("[webrtc] Recv message: %s", message)

		var parsedMessage webrtcMessage
		if err = json.Unmarshal([]byte(message), &parsedMessage); nil != err {
			errMessage = "[webrtc] Error parsing message:"
			log.Println(errMessage, err)
			continue
		}

		wrongStateErrMessage := "[webrtc] Called '%s' command in wrong state: %s. State should been: %s"
		switch parsedMessage.Command {
		case "start":
			if notStarted != handle.state {
				log.Printf(wrongStateErrMessage, parsedMessage.Command, handle.state, notStarted)
				continue
			}

			if err = handle.initializePeerConnection(); nil != err {
				errMessage = "[webrtc] Error initializing peer connection:"
				log.Println(errMessage, err)
				handle.state = failed
				continue
			}
			log.Println("[webrtc] Successfully initialized peer connection")
			handle.state = readyToBegin
			handle.send(webrtcMessage{Command: "ready"})

		case "offer":
			if readyToBegin != handle.state {
				log.Printf(wrongStateErrMessage, parsedMessage.Command, handle.state, readyToBegin)
				continue
			}

			handle.state = signalling
			if err = handle.takeOffer(parsedMessage.Sdp); nil != err {
				log.Println("[webrtc] Error taking offer:", err)
				handle.state = failed
				continue
			}

			answer := handle.peerConn.LocalDescription()
			err = handle.send(webrtcMessage{Command: "answer", Sdp: answer.SDP})
			if nil != err {
				log.Println("[webrtc] Error signalling answer:", err)
				handle.state = failed
				continue
			}

		case "candidate":
			if signalling != handle.state {
				log.Printf(wrongStateErrMessage, parsedMessage.Command, handle.state, signalling)
				continue
			}

			if err = handle.takeCandidate(parsedMessage.Candidate); nil != err {
				log.Println("[webrtc] Error taking candidate:", err)
				continue
			}
		default:
			log.Printf("[webrtc] Received unknown command '%s'. Ignoring...", parsedMessage.Command)
		}
	}
}

func (handle *webrtcHandle) initializePeerConnection() error {
	var err error
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
	if handle.peerConn, err = webrtcAPI.NewPeerConnection(config); nil != err {
		return err
	}

	for _, transceiverKind := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeAudio, webrtc.RTPCodecTypeVideo} {
		if _, err = handle.peerConn.AddTransceiverFromKind(transceiverKind); nil != err {
			return err
		}
	}

	handle.setupOnIceCandidate()
	handle.setupOnTrack()
	return nil
}

func (handle *webrtcHandle) takeOffer(offerSdp string) error {
	offer := webrtc.SessionDescription{
		SDP:  offerSdp,
		Type: webrtc.SDPTypeOffer,
	}

	if err := handle.peerConn.SetRemoteDescription(offer); nil != err {
		return err
	}

	answer, err := handle.peerConn.CreateAnswer(nil)
	if nil != err {
		return err
	}

	return handle.peerConn.SetLocalDescription(answer)
}

func (handle *webrtcHandle) takeCandidate(candidate string) error {
	return handle.peerConn.AddICECandidate(webrtc.ICECandidateInit{Candidate: candidate})
}

func (handle *webrtcHandle) setupOnIceCandidate() {
	handle.peerConn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}

		if err := handle.send(webrtcMessage{
			Command:       "candidate",
			Candidate:     candidate.ToJSON().Candidate,
			SDPMid:        *candidate.ToJSON().SDPMid,
			SDPMLineIndex: candidate.ToJSON().SDPMLineIndex,
		}); err != nil {
			log.Println("[webrtc] Error signalling candidate:", err)
			return
		}
	})
}

func (handle *webrtcHandle) setupOnTrack() {
	handle.peerConn.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Println("[webrtc] Received Track!!")
	})
}

func (handle *webrtcHandle) send(messageStruct webrtcMessage) error {
	message, _ := json.Marshal(messageStruct)
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	return handle.wsConn.WriteMessage(websocket.TextMessage, message)
}
