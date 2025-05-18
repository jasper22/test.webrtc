import { Injectable } from '@angular/core';
import { Subject } from 'rxjs';

@Injectable({
  providedIn: 'root'
})
export class WebrtcService {
  private peerConnection: RTCPeerConnection | null = null;
  private websocket: WebSocket | null = null;
  private remoteStreamSubject = new Subject<MediaStream>();
  public remoteStream$ = this.remoteStreamSubject.asObservable();

  constructor() { }

  async start() {
    console.log("Starting WebRTC connection");
    this.websocket = new WebSocket("ws://localhost:8080/ws");

    this.websocket.onopen = () => {
      console.log("WebSocket connected");
      this.websocket?.send("offerreq");
    };

    this.websocket.onmessage = async (event) => {
      console.log("WebSocket message received:", event.data);
      const message = event.data;

      if (!this.peerConnection) {
        await this.createPeerConnection();
      }

      try {
        const offer = new RTCSessionDescription({ type: 'offer', sdp: message });
        await this.peerConnection?.setRemoteDescription(offer);
        console.log("Remote description set (offer)");

        const answer = await this.peerConnection?.createAnswer();
        await this.peerConnection?.setLocalDescription(answer);
        console.log("Local description set (answer)");

        this.websocket?.send(answer?.sdp || '');
        console.log("Sent answer SDP to server");

      } catch (e) {
        console.error("Error handling WebSocket message:", e);
        this.stop();
      }
    };

    this.websocket.onerror = (error) => {
      console.error("WebSocket error:", error);
      this.stop();
    };

    this.websocket.onclose = () => {
      console.log("WebSocket closed");
      this.stop();
    };
  }

  private async createPeerConnection() {
    console.log("Creating RTCPeerConnection");
    this.peerConnection = new RTCPeerConnection({
      iceServers: [
        {
          urls: 'stun:stun.l.google.com:19302'
        }
      ]
    });

    this.peerConnection.onicecandidate = (event) => {
      if (event.candidate) {
        console.log("ICE candidate generated:", event.candidate);
        this.websocket?.send(JSON.stringify({ candidate: event.candidate }));
      } else {
        console.log("ICE gathering complete.");
      }
    };

    this.peerConnection.ontrack = (event) => {
      console.log("Track received:", event.track);
      console.log("Received streams:", event.streams);
      if (event.streams && event.streams[0]) {
        this.remoteStreamSubject.next(event.streams[0]);
        console.log("Remote video stream available");
      } else {
        console.warn("Received track event with no streams or empty streams array.");
      }
    };

    this.peerConnection.oniceconnectionstatechange = () => {
      console.log("ICE connection state:", this.peerConnection?.iceConnectionState);
    };

    this.peerConnection.onconnectionstatechange = () => {
      console.log("Connection state:", this.peerConnection?.connectionState);
      if (this.peerConnection?.connectionState === 'disconnected' || this.peerConnection?.connectionState === 'failed' || this.peerConnection?.connectionState === 'closed') {
        console.log("PeerConnection state changed to disconnected, failed, or closed. Stopping.");
        this.stop();
      }
    };

    console.log("RTCPeerConnection created.");
  }

  stop() {
    console.log("Stopping WebRTC connection");
    if (this.websocket) {
      this.websocket.close();
      this.websocket = null;
    }

    if (this.peerConnection) {
      this.peerConnection.close();
      this.peerConnection = null;
    }

    // Signal that the stream is no longer available
    this.remoteStreamSubject.next(null as any); // Use null or an empty stream as a signal
    console.log("Stopped");
  }
}