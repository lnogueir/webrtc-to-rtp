package wshandles

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/lithammer/shortuuid"
	"github.com/pion/rtp"
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
	baseHandle
	state    webrtcState
	peerConn *webrtc.PeerConnection
}

//WebRTCHandle handles webrtc related requests
func WebRTCHandle(w http.ResponseWriter, r *http.Request) {
	var err error
	handle := webrtcHandle{
		baseHandle: baseHandle{id: shortuuid.New()},
		state:      notStarted,
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
				log.Printf("[webrtc=%s] Disconnecting", handle.id)
				return
			}
			errMessage = fmt.Sprintf("[webrtc=%s] Error reading message: %s", handle.id, err)
			log.Println(errMessage)
			continue
		}
		log.Printf("[webrtc=%s] recv: %s", handle.id, message)

		var parsedMessage webrtcMessage
		if err = json.Unmarshal([]byte(message), &parsedMessage); nil != err {
			errMessage = fmt.Sprintf("[webrtc=%s] Error parsing message: %s", handle.id, err)
			log.Println(errMessage)
			continue
		}

		wrongStateErrMessage := "Called '%s' command in wrong state: %s. State should be: %s. Message ignored."
		switch parsedMessage.Command {
		case "start":
			if notStarted != handle.state {
				errMessage = fmt.Sprintf(wrongStateErrMessage, parsedMessage.Command, handle.state, notStarted)
				log.Printf("[webrtc=%s] %s", handle.id, errMessage)
				handle.sendWarning(errMessage)
				continue
			}

			if err = handle.initializePeerConnection(); nil != err {
				errMessage = fmt.Sprintf("Error initializing peer connection: %s", err)
				handle.mutex.Lock()
				handle.state = failed
				handle.mutex.Unlock()
				log.Printf("[webrtc=%s] %s", handle.id, errMessage)
				handle.sendError(errMessage)
				return
			}
			log.Printf("[webrtc=%s] Successfully initialized peer connection", handle.id)
			handle.mutex.Lock()
			handle.state = readyToBegin
			handle.mutex.Unlock()
			handle.send(webrtcMessage{Command: "ready"})

		case "offer":
			if readyToBegin != handle.state {
				errMessage = fmt.Sprintf(wrongStateErrMessage, parsedMessage.Command, handle.state, readyToBegin)
				log.Printf("[webrtc=%s] %s", handle.id, errMessage)
				handle.sendWarning(errMessage)
				continue
			}

			handle.mutex.Lock()
			handle.state = signalling
			handle.mutex.Unlock()

			if err = handle.takeOffer(parsedMessage.Sdp); nil != err {
				errMessage = fmt.Sprintf("Error taking offer: %s", err)
				handle.mutex.Lock()
				handle.state = failed
				handle.mutex.Unlock()
				log.Printf("[webrtc=%s] %s", handle.id, errMessage)
				handle.sendError(errMessage)
				return
			}

			answer := handle.peerConn.LocalDescription()
			log.Println(answer.SDP)
			handle.send(webrtcMessage{Command: "answer", Sdp: answer.SDP})

		case "candidate":
			if signalling != handle.state {
				errMessage = fmt.Sprintf(wrongStateErrMessage, parsedMessage.Command, handle.state, signalling)
				log.Printf("[webrtc=%s] %s", handle.id, errMessage)
				handle.sendWarning(errMessage)
				continue
			}

			if err = handle.takeCandidate(parsedMessage.Candidate); nil != err {
				errMessage = fmt.Sprintf("Error taking candidate: %s", err)
				log.Printf("[webrtc=%s] %s", handle.id, errMessage)
				handle.sendWarning(errMessage)
				continue
			}
		default:
			errMessage = fmt.Sprintf("Received unknown command '%s'. Ignored.", parsedMessage.Command)
			log.Printf("[webrtc=%s] %s", handle.id, errMessage)
			handle.sendWarning(errMessage)
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

	handle.setupOnConnectionStateChange()
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

		handle.send(webrtcMessage{
			Command:       "candidate",
			Candidate:     candidate.ToJSON().Candidate,
			SDPMid:        *candidate.ToJSON().SDPMid,
			SDPMLineIndex: candidate.ToJSON().SDPMLineIndex,
		})
	})
}

func (handle *webrtcHandle) setupOnConnectionStateChange() {
	handle.peerConn.OnConnectionStateChange(func(pcState webrtc.PeerConnectionState) {
		switch pcState {
		case webrtc.PeerConnectionStateConnected:
			handle.mutex.Lock()
			handle.state = streaming
			handle.mutex.Unlock()
			// here I have to figure out how to generate SDP for UDP stream and send it through ws
		case webrtc.PeerConnectionStateFailed:
			handle.mutex.Lock()
			handle.state = failed
			handle.mutex.Unlock()
			fallthrough
		case webrtc.PeerConnectionStateClosed:
			fallthrough
		case webrtc.PeerConnectionStateDisconnected:
			// Here I should handle when user connection gets diconnected
		}
	})
}

func (handle *webrtcHandle) setupOnTrack() {
	handle.peerConn.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[webrtc=%s] Received %s track", handle.id, track.Kind())
		var err error
		var errMessage string
		var udpConn *net.UDPConn
		var raddr *net.UDPAddr
		var payloadType uint8
		switch track.Kind() {
		case webrtc.RTPCodecTypeAudio:
			payloadType = 111
			raddr, err = net.ResolveUDPAddr("udp", "127.0.0.1:4000")
			if nil != err {
				log.Printf("[webrtc=%s] net.ResolveUDPAddr returned error: %s", handle.id, err)
			}
		case webrtc.RTPCodecTypeVideo:
			payloadType = 96
			raddr, err = net.ResolveUDPAddr("udp", "127.0.0.1:4002")
			if nil != err {
				log.Printf("[webrtc=%s] net.ResolveUDPAddr returned error: %s", handle.id, err)
			}
		}
		laddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:")
		udpConn, err = net.DialUDP("udp", laddr, raddr)
		if nil != err {
			errMessage = fmt.Sprintf("Error creating UDP connection: %s", err)
			log.Printf("[webrtc=%s] %s", handle.id, errMessage)
			handle.sendError(errMessage)
			return
		}

		packetBytes := make([]byte, 1500)
		rtpPacket := &rtp.Packet{}
		for {
			// Read
			n, _, err := track.Read(packetBytes)
			if nil != err {
				log.Printf("[webrtc=%s] track.Read returned error: %s", handle.id, err)
			}

			// Unmarshal the packet and update the PayloadType
			if err = rtpPacket.Unmarshal(packetBytes[:n]); nil != err {
				log.Printf("[webrtc=%s] rtpPacket.Unmarshal returned error: %s", handle.id, err)
			}
			rtpPacket.PayloadType = payloadType

			// Marshal into original buffer with updated PayloadType
			if n, err = rtpPacket.MarshalTo(packetBytes); err != nil {
				log.Printf("[webrtc=%s] rtpPacket.MarshalTo returned error: %s", handle.id, err)
			}

			// Write
			if _, err = udpConn.Write(packetBytes[:n]); err != nil {
				if opError, ok := err.(*net.OpError); ok && opError.Err.Error() == "write: connection refused" {
					continue
				}
				log.Printf("[webrtc=%s] udpConn.Write returned error: %s", handle.id, err)
			}
			// log.Printf("[webrtc=%s] Sent %d bytes RTP packet", handle.id, n)
		}
	})
}
