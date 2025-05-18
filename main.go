package main

import (
	"encoding/json" // Package to handle JSON encoding and decoding [1]
	"io"            // Package for basic I/O interfaces [1]
	"log"           // Package for logging messages [1]
	"net/http"      // Package for building HTTP servers and clients [1]
	"os/exec"       // Package to run external commands [1]
	"sync"          // Package for synchronization primitives like mutexes [1]
	"time"          // Package for measuring and displaying time [1]

	"github.com/gorilla/websocket"                  // Package for WebSocket communication [1]
	"github.com/pion/webrtc/v4"                     // The Pion WebRTC API [1]
	"github.com/pion/webrtc/v4/pkg/media"           // Package for media handling in WebRTC [1]
	"github.com/pion/webrtc/v4/pkg/media/ivfreader" // IVF (Indeo Video Format) reader for parsing video data [1]
	// Added import
)

var (
	// upgrader is used to upgrade HTTP connections to WebSocket connections [1]
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for simplicity in this example; *DO NOT* do this in production.  In production, you would want to check the origin against a whitelist.
		},
	}
	mu             sync.Mutex                     // Mutex to protect shared resources [1]
	peerConnection *webrtc.PeerConnection         // Represents the WebRTC peer connection [1]
	videoTrack     *webrtc.TrackLocalStaticSample // Represents the local video track [1]
	ffmpegCmd      *exec.Cmd                      // Represents the FFmpeg command [1]
)

func main() {
	// Create HTTP server
	http.HandleFunc("/ws", handleWebSocket) // Handle WebSocket connections at the "/ws" endpoint
	log.Println("Server listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil { // Start the HTTP server on port 8080
		log.Fatalf("Failed to start server: %v", err)
	}
}

// handleWebSocket handles WebSocket connections from clients [1]
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close() // Ensure the connection is closed when the function exits

	log.Println("Client connected")

	// 1.  Create a new WebRTC API instance.  This is the entry point to the WebRTC API [14]
	api := webrtc.NewAPI()

	// 2. Set up MediaEngine
	mediaEngine := webrtc.MediaEngine{}                            // A MediaEngine defines the capabilities of the PeerConnection
	if err := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{ // Register the VP8 codec [13]
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP8, // Use VP8 for video [13]
			ClockRate:   90000,              // Clock rate for VP8 is 90kHz
			Channels:    1,                  // Mono audio
			SDPFmtpLine: "",                 // No specific parameters for VP8
		},
		PayloadType: 96, // Important: Make sure this PayloadType is consistent.  This is the identifier for the codec in the RTP stream.
	}, webrtc.RTPCodecTypeVideo); err != nil { // Specify that this is a video codec
		log.Fatalf("error registering VP8 codec: %v", err)
	}

	// 3.  Create PeerConnection
	peerConnection, err = api.NewPeerConnection(webrtc.Configuration{ // Create a new PeerConnection with an empty configuration
		//ICEServers: []webrtc.ICEServer{  // In a real application, you would want to specify STUN/TURN servers here for better connectivity.
		// STUN servers allow clients to discover their public IP address, while TURN servers relay traffic when direct peer-to-peer connections are not possible.
		//	{
		//		URLs: []string{"stun:stun.l.google.com:19302"},
		//	},
		//},
	})

	if err != nil {
		log.Fatalf("error creating peer connection: %v", err)
	}

	defer func() {
		mu.Lock()
		if peerConnection != nil {
			peerConnection.Close() // Close the peer connection when the function exits
		}
		mu.Unlock()
	}()

	// 4. Create a video track
	videoTrack, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "screen-share") // Create a new track for sending video [1]
	if err != nil {
		log.Fatalf("error creating video track: %v", err)
	}

	// 5. Add the track to the PeerConnection
	sender, err := peerConnection.AddTrack(videoTrack) // Add the video track to the peer connection [1]
	if err != nil {
		log.Fatalf("error adding track: %v", err)
	}

	// 6. Handle track negotiation.  This is important for receiving RTCP packets, which provide feedback on the quality of the media stream. [1]
	go func() {
		rtcpBuf := make([]byte, 1500) // Create a buffer for receiving RTCP packets
		for {
			if _, _, rtcpErr := sender.Read(rtcpBuf); rtcpErr != nil { // Read RTCP packets from the sender
				return
			}
			// In a real application, you would process the RTCP packets here to get feedback on the media stream.
		}
	}()

	// 7. Start screen capture and stream it to WebRTC
	go captureAndStream(videoTrack) // Start capturing the screen and streaming it to the video track

	// 8.  Handle WebSocket messages (signaling).  Signaling is the process of exchanging metadata between peers to establish a WebRTC connection. [4, 5]
	for {
		messageType, payload, err := conn.ReadMessage() // Read messages from the WebSocket connection
		if err != nil {
			log.Printf("error reading message: %v", err)
			return
		}

		switch messageType {
		case websocket.TextMessage: // Handle text messages
			handleSignalingMessage(conn, string(payload))
		case websocket.CloseMessage: // Handle close messages
			log.Println("Client disconnected")
			return
		}
	}
}

// handleSignalingMessage handles signaling messages received from the client [4, 5]
func handleSignalingMessage(conn *websocket.Conn, message string) {
	mu.Lock()         // Acquire a lock to protect shared resources
	defer mu.Unlock() // Release the lock when the function exits

	if peerConnection == nil {
		log.Println("Peer Connection is nil, cannot handle message:", message)
		return
	}

	log.Printf("Received signaling message: %s\n", message)
	msg := []byte(message)

	// Try to unmarshal as ICE candidate.  ICE candidates are potential addresses that peers can use to connect to each other. [5]
	var candidateMsg struct {
		Candidate *webrtc.ICECandidateInit `json:"candidate"`
	}

	if err := json.Unmarshal(msg, &candidateMsg); err == nil && candidateMsg.Candidate != nil {
		// It's an ICE candidate
		log.Printf("Received ICE candidate: %+v\n", candidateMsg.Candidate)
		if err := peerConnection.AddICECandidate(*candidateMsg.Candidate); err != nil { // Add the ICE candidate to the peer connection
			log.Printf("error adding ICE candidate: %v", err)
		}
		return // Handled the message
	}

	// If not an ICE candidate, assume it's an SDP message (offerreq or answer).  SDP (Session Description Protocol) is used to negotiate the media capabilities of the peers. [6]
	if len(msg) > 0 {
		if string(msg[0:8]) == "offerreq" { // The client is requesting an offer
			offer, err := peerConnection.CreateOffer(nil) // Create an offer [6]
			if err != nil {
				log.Printf("error creating offer: %v", err)
				return
			}
			err = peerConnection.SetLocalDescription(offer) // Set the local description to the offer [6]
			if err != nil {
				log.Printf("error setting local description: %v", err)
				return
			}
			err = conn.WriteMessage(websocket.TextMessage, []byte(offer.SDP)) // Send the offer to the client [6]
			if err != nil {
				log.Printf("error sending offer: %v", err)
				return
			}

		} else {
			// Assume it's the answer SDP
			err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(msg)}) // Set the remote description to the answer [6]
			if err != nil {
				log.Printf("error setting remote description: %v", err)
				return
			}
		}
	}
}

// captureAndStream captures the screen using FFmpeg and streams it to the WebRTC video track [3, 7]
func captureAndStream(track *webrtc.TrackLocalStaticSample) {
	// 1. Initialize screen capture (OS-specific - Windows example using gdigrab with ffmpeg).  FFmpeg is used to capture the screen and encode it into VP8. [3]
	// Use libvpx for VP8 encoding and output in IVF format
	cmd := exec.Command("ffmpeg",
		"-f", "gdigrab", // Use gdigrab for screen capture on Windows [3, 7]
		"-video_size", "1920x1080", // Set the desired capture resolution.  This should match the screen resolution. [3]
		"-framerate", "30", // Capture at 30 FPS [3]
		"-i", "desktop", // Capture the entire desktop [3]
		"-pix_fmt", "yuv420p", // Specify pixel format.  yuv420p is a common pixel format for video. [3]
		"-c:v", "libvpx", // Use libvpx for VP8 encoding [3]
		"-b:v", "1M", // Set the video bitrate [3]
		"-bufsize", "1M",
		"-threads", "4",
		"-deadline", "realtime",
		"-f", "ivf", // Output in IVF format [3]
		"-") // Output to stdout [3]

	ffmpegCmd = cmd
	stdout, err := cmd.StdoutPipe() // Get the standard output pipe from FFmpeg
	if err != nil {
		log.Fatalf("error creating stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe() // Get stderr pipe.  This is useful for debugging FFmpeg.
	if err != nil {
		log.Fatalf("error creating stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil { // Start the FFmpeg command
		log.Fatalf("error starting ffmpeg: %v", err)
	}

	// Goroutine to read and log FFmpeg stderr
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("error reading ffmpeg stderr: %v", err)
				}
				return
			}
			if n > 0 {
				log.Printf("FFmpeg stderr: %s", string(buf[:n]))
			}
		}
	}()

	defer func() {
		if ffmpegCmd != nil {
			ffmpegCmd.Process.Kill() // Kill the FFmpeg process when the function exits
			ffmpegCmd.Wait()         // Wait for the FFmpeg process to exit
		}
	}()

	// 2. Read IVF packets from ffmpeg and send to WebRTC.  The IVF reader parses the IVF stream and extracts the VP8 frames.
	ivf, _, err := ivfreader.NewWith(stdout) // Create a new IVF reader from the FFmpeg stdout pipe
	if err != nil {
		log.Fatalf("error creating ivf reader: %v", err)
	}

	for {
		// Read the next IVF frame
		frame, _, err := ivf.ParseNextFrame() // Parse the next IVF frame [1]
		if err != nil {
			log.Printf("error reading IVF frame from ffmpeg: %v", err)
			return // Exit the capture loop on error
		}

		// Write the frame to the video track
		err = track.WriteSample(media.Sample{ // Write the frame to the video track [1]
			Data:     frame,                 // The VP8 encoded frame data
			Duration: time.Millisecond * 33, // Assuming 30 FPS.  The duration of the frame.
		})
		if err != nil {
			log.Printf("error writing to video track: %v", err)
			return
		}
	}
}
