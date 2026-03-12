# React Native WebRTC Integration Guide

คู่มือการใช้งาน K2 Gateway กับ React Native สำหรับการโทร SIP/WebRTC

## Prerequisites

### Dependencies

```bash
npm install react-native-webrtc
# หรือ
yarn add react-native-webrtc
```

### iOS Setup

```bash
cd ios && pod install
```

เพิ่มใน `Info.plist`:

```xml
<key>NSCameraUsageDescription</key>
<string>Camera access for video calls</string>
<key>NSMicrophoneUsageDescription</key>
<string>Microphone access for voice calls</string>
```

### Android Setup

เพิ่มใน `android/app/src/main/AndroidManifest.xml`:

```xml
<uses-permission android:name="android.permission.CAMERA" />
<uses-permission android:name="android.permission.RECORD_AUDIO" />
<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS" />
```

---

## Basic Implementation

### 1. WebRTC Service

```typescript
// src/services/WebRTCService.ts
import { RTCPeerConnection, RTCSessionDescription, mediaDevices, MediaStream, RTCView } from "react-native-webrtc";

const ICE_SERVERS = {
  iceServers: [
    {
      urls: "turn:turn.ttrs.or.th:3478?transport=udp",
      username: "turn01",
      credential: "Test1234",
    },
  ],
};

class WebRTCService {
  private ws: WebSocket | null = null;
  private pc: RTCPeerConnection | null = null;
  private localStream: MediaStream | null = null;
  private sessionId: string | null = null;

  // Callbacks
  public onLocalStream: ((stream: MediaStream) => void) | null = null;
  public onRemoteStream: ((stream: MediaStream) => void) | null = null;
  public onCallState: ((state: string) => void) | null = null;
  public onMessage: ((from: string, body: string) => void) | null = null;
  public onIncomingCall: ((from: string, sessionId: string) => void) | null = null;
  public onRegistered: ((registered: boolean) => void) | null = null;
  public onDTMF: ((digit: string, sessionId: string) => void) | null = null;

  // Connect to K2 Gateway WebSocket
  async connect(wsUrl: string): Promise<void> {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        console.log("WebSocket connected");
        resolve();
      };

      this.ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        reject(error);
      };

      this.ws.onclose = () => {
        console.log("WebSocket disconnected");
        this.cleanup();
      };

      this.ws.onmessage = (event) => {
        this.handleMessage(JSON.parse(event.data));
      };
    });
  }

  // Handle incoming WebSocket messages
  private handleMessage(msg: any): void {
    switch (msg.type) {
      case "answer":
        this.handleAnswer(msg);
        break;
      case "state":
        if (msg.sessionId) this.sessionId = msg.sessionId;
        this.onCallState?.(msg.state);
        break;
      case "incoming":
        this.onIncomingCall?.(msg.from, msg.sessionId);
        break;
      case "registerStatus":
        this.onRegistered?.(msg.registered);
        break;
      case "message":
        this.onMessage?.(msg.from, msg.body);
        break;
      case "dtmf":
        this.onDTMF?.(msg.digits || msg.digit, msg.sessionId);
        break;
      case "error":
        console.error("Server error:", msg.error);
        break;
    }
  }

  // Start media session
  async startSession(): Promise<void> {
    try {
      // Get local media
      this.localStream = await mediaDevices.getUserMedia({
        audio: true,
        video: {
          width: 640,
          height: 480,
          frameRate: 30,
          facingMode: "user",
        },
      });

      this.onLocalStream?.(this.localStream);

      // Create peer connection
      this.pc = new RTCPeerConnection(ICE_SERVERS);

      // Add local tracks
      this.localStream.getTracks().forEach((track) => {
        this.pc!.addTrack(track, this.localStream!);
      });

      // Handle remote tracks
      this.pc.ontrack = (event) => {
        if (event.streams && event.streams[0]) {
          this.onRemoteStream?.(event.streams[0]);
        }
      };

      // Create and send offer
      const offer = await this.pc.createOffer({});
      await this.pc.setLocalDescription(offer);

      // Wait for ICE gathering
      await this.waitForIceGathering();

      // Send offer to server
      this.send({
        type: "offer",
        sdp: this.pc.localDescription?.sdp,
      });
    } catch (error) {
      console.error("Failed to start session:", error);
      throw error;
    }
  }

  // Wait for ICE candidates to be gathered
  private waitForIceGathering(): Promise<void> {
    return new Promise((resolve) => {
      if (this.pc?.iceGatheringState === "complete") {
        resolve();
        return;
      }

      const timeout = setTimeout(resolve, 2000);

      this.pc!.onicegatheringstatechange = () => {
        if (this.pc?.iceGatheringState === "complete") {
          clearTimeout(timeout);
          resolve();
        }
      };
    });
  }

  // Handle SDP answer from server
  private async handleAnswer(msg: any): Promise<void> {
    if (!this.pc) return;

    try {
      await this.pc.setRemoteDescription(new RTCSessionDescription({ type: "answer", sdp: msg.sdp }));
      this.sessionId = msg.sessionId;
      console.log("Session established:", this.sessionId);
    } catch (error) {
      console.error("Failed to set remote description:", error);
    }
  }

  // SIP Registration
  register(domain: string, username: string, password: string, port: number = 5060): void {
    this.send({
      type: "register",
      sipDomain: domain,
      sipUsername: username,
      sipPassword: password,
      sipPort: port,
    });
  }

  unregister(): void {
    this.send({ type: "unregister" });
  }

  // Call controls
  call(destination: string): void {
    if (!this.sessionId) {
      console.error("No active session");
      return;
    }

    this.send({
      type: "call",
      sessionId: this.sessionId,
      destination,
    });
  }

  hangup(): void {
    if (!this.sessionId) return;

    this.send({
      type: "hangup",
      sessionId: this.sessionId,
    });
  }

  acceptCall(incomingSessionId: string): void {
    // Incoming flow (API mode): create/maintain WebRTC session via offer/answer first,
    // then send accept with the inbound SIP sessionId.
    this.send({
      type: "accept",
      sessionId: incomingSessionId,
    });
  }

  rejectCall(incomingSessionId: string): void {
    this.send({
      type: "reject",
      sessionId: incomingSessionId,
    });
  }

  // DTMF
  sendDTMF(digits: string): void {
    if (!this.sessionId) return;

    this.send({
      type: "dtmf",
      sessionId: this.sessionId,
      digits,
    });
  }

  // SIP MESSAGE
  sendMessage(body: string, contentType: string = "text/plain;charset=UTF-8"): void {
    this.send({
      type: "send_message",
      body,
      contentType,
    });
  }

  // Mute controls
  toggleAudioMute(): boolean {
    if (!this.localStream) return false;

    const audioTrack = this.localStream.getAudioTracks()[0];
    if (audioTrack) {
      audioTrack.enabled = !audioTrack.enabled;
      return !audioTrack.enabled; // Return true if now muted
    }
    return false;
  }

  toggleVideoMute(): boolean {
    if (!this.localStream) return false;

    const videoTrack = this.localStream.getVideoTracks()[0];
    if (videoTrack) {
      videoTrack.enabled = !videoTrack.enabled;
      return !videoTrack.enabled; // Return true if now muted
    }
    return false;
  }

  // Switch camera (front/back)
  async switchCamera(): Promise<void> {
    if (!this.localStream) return;

    const videoTrack = this.localStream.getVideoTracks()[0];
    if (videoTrack) {
      // @ts-ignore - _switchCamera is available in react-native-webrtc
      videoTrack._switchCamera();
    }
  }

  // Helper to send WebSocket messages
  private send(data: any): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  // Cleanup
  cleanup(): void {
    if (this.localStream) {
      this.localStream.getTracks().forEach((track) => track.stop());
      this.localStream = null;
    }

    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }

    this.sessionId = null;
  }

  disconnect(): void {
    this.cleanup();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }
}

export default new WebRTCService();
```

---

## Reconnection Support

### WebRTC Service with Auto Reconnect

```typescript
// src/services/WebRTCServiceWithReconnect.ts
import { RTCPeerConnection, RTCSessionDescription, mediaDevices, MediaStream } from "react-native-webrtc";

const ICE_SERVERS = {
  iceServers: [
    {
      urls: "turn:turn.ttrs.or.th:3478?transport=udp",
      username: "turn01",
      credential: "Test1234",
    },
  ],
};

// Reconnection configuration
interface ReconnectConfig {
  maxAttempts: number; // Maximum retry attempts (default: 5)
  baseDelay: number; // Initial delay in ms (default: 1000)
  maxDelay: number; // Maximum delay in ms (default: 30000)
  backoffMultiplier: number; // Exponential backoff multiplier (default: 2)
}

type ConnectionState = "disconnected" | "connecting" | "connected" | "reconnecting";

class WebRTCService {
  private ws: WebSocket | null = null;
  private pc: RTCPeerConnection | null = null;
  private localStream: MediaStream | null = null;
  private sessionId: string | null = null;

  // Reconnection state
  private wsUrl: string = "";
  private reconnectAttempts: number = 0;
  private reconnectTimer: NodeJS.Timeout | null = null;
  private isManualDisconnect: boolean = false;
  private connectionState: ConnectionState = "disconnected";

  private reconnectConfig: ReconnectConfig = {
    maxAttempts: 5,
    baseDelay: 1000,
    maxDelay: 30000,
    backoffMultiplier: 2,
  };

  // SIP credentials for re-registration after reconnect
  private sipCredentials: {
    domain: string;
    username: string;
    password: string;
    port: number;
  } | null = null;

  // Callbacks
  public onLocalStream: ((stream: MediaStream) => void) | null = null;
  public onRemoteStream: ((stream: MediaStream) => void) | null = null;
  public onCallState: ((state: string) => void) | null = null;
  public onMessage: ((from: string, body: string) => void) | null = null;
  public onIncomingCall: ((from: string, sessionId: string) => void) | null = null;
  public onRegistered: ((registered: boolean) => void) | null = null;
  public onDTMF: ((digit: string, sessionId: string) => void) | null = null;

  // New callbacks for connection state
  public onConnectionStateChange: ((state: ConnectionState) => void) | null = null;
  public onReconnecting: ((attempt: number, maxAttempts: number) => void) | null = null;
  public onReconnectFailed: (() => void) | null = null;

  // Configure reconnection settings
  setReconnectConfig(config: Partial<ReconnectConfig>): void {
    this.reconnectConfig = { ...this.reconnectConfig, ...config };
  }

  // Calculate delay with exponential backoff
  private getReconnectDelay(): number {
    const delay = Math.min(
      this.reconnectConfig.baseDelay * Math.pow(this.reconnectConfig.backoffMultiplier, this.reconnectAttempts),
      this.reconnectConfig.maxDelay
    );
    // Add jitter (±20%) to prevent thundering herd
    const jitter = delay * 0.2 * (Math.random() * 2 - 1);
    return Math.round(delay + jitter);
  }

  // Update connection state
  private setConnectionState(state: ConnectionState): void {
    this.connectionState = state;
    this.onConnectionStateChange?.(state);
  }

  // Connect to K2 Gateway WebSocket with reconnection support
  async connect(wsUrl: string): Promise<void> {
    this.wsUrl = wsUrl;
    this.isManualDisconnect = false;
    this.reconnectAttempts = 0;

    return this.establishConnection();
  }

  private async establishConnection(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.setConnectionState(this.reconnectAttempts > 0 ? "reconnecting" : "connecting");

      this.ws = new WebSocket(this.wsUrl);

      this.ws.onopen = () => {
        console.log("WebSocket connected");
        this.reconnectAttempts = 0;
        this.setConnectionState("connected");

        // Re-register if we have stored credentials
        if (this.sipCredentials) {
          console.log("Re-registering after reconnect...");
          this.register(this.sipCredentials.domain, this.sipCredentials.username, this.sipCredentials.password, this.sipCredentials.port);
        }

        resolve();
      };

      this.ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        if (this.reconnectAttempts === 0) {
          reject(error);
        }
      };

      this.ws.onclose = (event) => {
        console.log("WebSocket disconnected:", event.code, event.reason);
        this.setConnectionState("disconnected");

        if (!this.isManualDisconnect) {
          this.scheduleReconnect();
        }
      };

      this.ws.onmessage = (event) => {
        this.handleMessage(JSON.parse(event.data));
      };
    });
  }

  // Schedule reconnection attempt
  private scheduleReconnect(): void {
    if (this.reconnectAttempts >= this.reconnectConfig.maxAttempts) {
      console.log("Max reconnect attempts reached");
      this.onReconnectFailed?.();
      return;
    }

    const delay = this.getReconnectDelay();
    this.reconnectAttempts++;

    console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts}/${this.reconnectConfig.maxAttempts})`);
    this.onReconnecting?.(this.reconnectAttempts, this.reconnectConfig.maxAttempts);

    this.reconnectTimer = setTimeout(async () => {
      try {
        await this.establishConnection();
        // Restart media session after reconnect
        if (this.localStream) {
          await this.startSession();
        }
      } catch (error) {
        console.error("Reconnect failed:", error);
        // Will automatically retry via onclose handler
      }
    }, delay);
  }

  // Cancel pending reconnection
  cancelReconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.reconnectAttempts = 0;
  }

  // Manual reconnect
  async reconnect(): Promise<void> {
    this.cancelReconnect();
    this.cleanup();
    this.reconnectAttempts = 0;
    this.isManualDisconnect = false;
    return this.establishConnection();
  }

  // Handle incoming WebSocket messages
  private handleMessage(msg: any): void {
    switch (msg.type) {
      case "answer":
        this.handleAnswer(msg);
        break;
      case "state":
        if (msg.sessionId) this.sessionId = msg.sessionId;
        this.onCallState?.(msg.state);
        break;
      case "incoming":
        this.onIncomingCall?.(msg.from, msg.sessionId);
        break;
      case "registerStatus":
        this.onRegistered?.(msg.registered);
        break;
      case "message":
        this.onMessage?.(msg.from, msg.body);
        break;
      case "dtmf":
        this.onDTMF?.(msg.digits || msg.digit, msg.sessionId);
        break;
      case "error":
        console.error("Server error:", msg.error);
        break;
    }
  }

  // Start media session
  async startSession(): Promise<void> {
    try {
      this.localStream = await mediaDevices.getUserMedia({
        audio: true,
        video: {
          width: 640,
          height: 480,
          frameRate: 30,
          facingMode: "user",
        },
      });

      this.onLocalStream?.(this.localStream);

      this.pc = new RTCPeerConnection(ICE_SERVERS);

      this.localStream.getTracks().forEach((track) => {
        this.pc!.addTrack(track, this.localStream!);
      });

      this.pc.ontrack = (event) => {
        if (event.streams && event.streams[0]) {
          this.onRemoteStream?.(event.streams[0]);
        }
      };

      const offer = await this.pc.createOffer({});
      await this.pc.setLocalDescription(offer);

      await this.waitForIceGathering();

      this.send({
        type: "offer",
        sdp: this.pc.localDescription?.sdp,
      });
    } catch (error) {
      console.error("Failed to start session:", error);
      throw error;
    }
  }

  private waitForIceGathering(): Promise<void> {
    return new Promise((resolve) => {
      if (this.pc?.iceGatheringState === "complete") {
        resolve();
        return;
      }

      const timeout = setTimeout(resolve, 2000);

      this.pc!.onicegatheringstatechange = () => {
        if (this.pc?.iceGatheringState === "complete") {
          clearTimeout(timeout);
          resolve();
        }
      };
    });
  }

  private async handleAnswer(msg: any): Promise<void> {
    if (!this.pc) return;

    try {
      await this.pc.setRemoteDescription(new RTCSessionDescription({ type: "answer", sdp: msg.sdp }));
      this.sessionId = msg.sessionId;
      console.log("Session established:", this.sessionId);
    } catch (error) {
      console.error("Failed to set remote description:", error);
    }
  }

  // SIP Registration (stores credentials for re-registration)
  register(domain: string, username: string, password: string, port: number = 5060): void {
    // Store credentials for reconnection
    this.sipCredentials = { domain, username, password, port };

    this.send({
      type: "register",
      sipDomain: domain,
      sipUsername: username,
      sipPassword: password,
      sipPort: port,
    });
  }

  unregister(): void {
    this.sipCredentials = null;
    this.send({ type: "unregister" });
  }

  // Call controls
  call(destination: string): void {
    if (!this.sessionId) {
      console.error("No active session");
      return;
    }
    this.send({
      type: "call",
      sessionId: this.sessionId,
      destination,
    });
  }

  hangup(): void {
    if (!this.sessionId) return;
    this.send({
      type: "hangup",
      sessionId: this.sessionId,
    });
  }

  acceptCall(incomingSessionId: string): void {
    // Incoming flow (API mode): create/maintain WebRTC session via offer/answer first,
    // then send accept with the inbound SIP sessionId.
    this.send({
      type: "accept",
      sessionId: incomingSessionId,
    });
  }

  rejectCall(incomingSessionId: string): void {
    this.send({
      type: "reject",
      sessionId: incomingSessionId,
    });
  }

  // DTMF
  sendDTMF(digits: string): void {
    if (!this.sessionId) return;
    this.send({
      type: "dtmf",
      sessionId: this.sessionId,
      digits,
    });
  }

  // SIP MESSAGE
  sendMessage(body: string, contentType: string = "text/plain;charset=UTF-8"): void {
    this.send({
      type: "send_message",
      body,
      contentType,
    });
  }

  // Mute controls
  toggleAudioMute(): boolean {
    if (!this.localStream) return false;
    const audioTrack = this.localStream.getAudioTracks()[0];
    if (audioTrack) {
      audioTrack.enabled = !audioTrack.enabled;
      return !audioTrack.enabled;
    }
    return false;
  }

  toggleVideoMute(): boolean {
    if (!this.localStream) return false;
    const videoTrack = this.localStream.getVideoTracks()[0];
    if (videoTrack) {
      videoTrack.enabled = !videoTrack.enabled;
      return !videoTrack.enabled;
    }
    return false;
  }

  async switchCamera(): Promise<void> {
    if (!this.localStream) return;
    const videoTrack = this.localStream.getVideoTracks()[0];
    if (videoTrack) {
      // @ts-ignore
      videoTrack._switchCamera();
    }
  }

  // Helper to send WebSocket messages
  private send(data: any): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    } else {
      console.warn("WebSocket not connected, message not sent:", data.type);
    }
  }

  // Get current connection state
  getConnectionState(): ConnectionState {
    return this.connectionState;
  }

  // Cleanup
  cleanup(): void {
    if (this.localStream) {
      this.localStream.getTracks().forEach((track) => track.stop());
      this.localStream = null;
    }

    if (this.pc) {
      this.pc.close();
      this.pc = null;
    }

    this.sessionId = null;
  }

  // Disconnect (manual - no reconnect)
  disconnect(): void {
    this.isManualDisconnect = true;
    this.cancelReconnect();
    this.cleanup();
    this.sipCredentials = null;

    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }
}

export default new WebRTCService();
```

### Using Reconnection in Component

```tsx
// src/screens/VideoCallScreen.tsx
import React, { useState, useEffect } from "react";
import { View, Text, Alert } from "react-native";
import WebRTCService from "../services/WebRTCServiceWithReconnect";

const VideoCallScreen: React.FC = () => {
  const [connectionState, setConnectionState] = useState<string>("disconnected");
  const [reconnectInfo, setReconnectInfo] = useState<string>("");

  useEffect(() => {
    // Configure reconnection
    WebRTCService.setReconnectConfig({
      maxAttempts: 5,
      baseDelay: 1000,
      maxDelay: 30000,
      backoffMultiplier: 2,
    });

    // Connection state callback
    WebRTCService.onConnectionStateChange = (state) => {
      setConnectionState(state);
      console.log("Connection state:", state);
    };

    // Reconnection progress callback
    WebRTCService.onReconnecting = (attempt, maxAttempts) => {
      setReconnectInfo(`Reconnecting... (${attempt}/${maxAttempts})`);
    };

    // Reconnection failed callback
    WebRTCService.onReconnectFailed = () => {
      setReconnectInfo("");
      Alert.alert("Connection Lost", "Unable to reconnect to server. Please try again.", [
        { text: "Retry", onPress: () => WebRTCService.reconnect() },
        { text: "Cancel", style: "cancel" },
      ]);
    };

    return () => {
      WebRTCService.disconnect();
    };
  }, []);

  return (
    <View>
      <Text>Status: {connectionState.toUpperCase()}</Text>
      {reconnectInfo && <Text>{reconnectInfo}</Text>}
      {/* ... rest of UI */}
    </View>
  );
};

export default VideoCallScreen;
```

---

## React Component Example

### 2. Video Call Screen

```tsx
// src/screens/VideoCallScreen.tsx
import React, { useState, useEffect } from "react";
import { View, Text, TouchableOpacity, StyleSheet, TextInput, Alert } from "react-native";
import { RTCView, MediaStream } from "react-native-webrtc";
import WebRTCService from "../services/WebRTCService";

const K2_GATEWAY_URL = "wss://k2-gateway.example.com/ws";

const VideoCallScreen: React.FC = () => {
  const [isConnected, setIsConnected] = useState(false);
  const [isRegistered, setIsRegistered] = useState(false);
  const [callState, setCallState] = useState("idle");
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [remoteStream, setRemoteStream] = useState<MediaStream | null>(null);
  const [destination, setDestination] = useState("9999");
  const [isAudioMuted, setIsAudioMuted] = useState(false);
  const [isVideoMuted, setIsVideoMuted] = useState(false);

  // SIP credentials
  const [sipDomain, setSipDomain] = useState("sipclient.ttrs.or.th");
  const [sipUsername, setSipUsername] = useState("");
  const [sipPassword, setSipPassword] = useState("");

  useEffect(() => {
    // Setup callbacks
    WebRTCService.onLocalStream = setLocalStream;
    WebRTCService.onRemoteStream = setRemoteStream;
    WebRTCService.onCallState = setCallState;
    WebRTCService.onRegistered = setIsRegistered;

    WebRTCService.onIncomingCall = (from, sessionId) => {
      Alert.alert("Incoming Call", `Call from: ${from}`, [
        { text: "Reject", onPress: () => WebRTCService.rejectCall(sessionId) },
        { text: "Accept", onPress: () => handleAcceptCall(sessionId) },
      ]);
    };

    WebRTCService.onMessage = (from, body) => {
      Alert.alert("Message", `From: ${from}\n${body}`);
    };

    return () => {
      WebRTCService.disconnect();
    };
  }, []);

  const handleConnect = async () => {
    try {
      await WebRTCService.connect(K2_GATEWAY_URL);
      setIsConnected(true);
      await WebRTCService.startSession();
    } catch (error) {
      Alert.alert("Error", "Failed to connect");
    }
  };

  const handleRegister = () => {
    WebRTCService.register(sipDomain, sipUsername, sipPassword);
  };

  const handleCall = () => {
    WebRTCService.call(destination);
  };

  const handleHangup = () => {
    WebRTCService.hangup();
  };

  const handleAcceptCall = async (sessionId: string) => {
    if (!localStream) {
      await WebRTCService.startSession();
    }
    WebRTCService.acceptCall(sessionId);
  };

  const toggleAudio = () => {
    const muted = WebRTCService.toggleAudioMute();
    setIsAudioMuted(muted);
  };

  const toggleVideo = () => {
    const muted = WebRTCService.toggleVideoMute();
    setIsVideoMuted(muted);
  };

  return (
    <View style={styles.container}>
      {/* Remote Video (Full Screen) */}
      {remoteStream && <RTCView streamURL={remoteStream.toURL()} style={styles.remoteVideo} objectFit="cover" />}

      {/* Local Video (PIP) */}
      {localStream && <RTCView streamURL={localStream.toURL()} style={styles.localVideo} objectFit="cover" mirror={true} />}

      {/* Controls Overlay */}
      <View style={styles.controls}>
        {!isConnected ? (
          <TouchableOpacity style={styles.button} onPress={handleConnect}>
            <Text style={styles.buttonText}>Connect</Text>
          </TouchableOpacity>
        ) : !isRegistered ? (
          <View style={styles.registerForm}>
            <TextInput style={styles.input} placeholder="SIP Domain" value={sipDomain} onChangeText={setSipDomain} />
            <TextInput style={styles.input} placeholder="Username" value={sipUsername} onChangeText={setSipUsername} />
            <TextInput style={styles.input} placeholder="Password" value={sipPassword} onChangeText={setSipPassword} secureTextEntry />
            <TouchableOpacity style={styles.button} onPress={handleRegister}>
              <Text style={styles.buttonText}>Register</Text>
            </TouchableOpacity>
          </View>
        ) : (
          <View style={styles.callControls}>
            <Text style={styles.stateText}>State: {callState.toUpperCase()}</Text>

            {callState === "idle" && (
              <>
                <TextInput
                  style={styles.input}
                  placeholder="Destination"
                  value={destination}
                  onChangeText={setDestination}
                  keyboardType="phone-pad"
                />
                <TouchableOpacity style={styles.callButton} onPress={handleCall}>
                  <Text style={styles.buttonText}>Call</Text>
                </TouchableOpacity>
              </>
            )}

            {(callState === "active" || callState === "ringing") && (
              <View style={styles.inCallButtons}>
                <TouchableOpacity style={[styles.controlButton, isAudioMuted && styles.mutedButton]} onPress={toggleAudio}>
                  <Text style={styles.buttonText}>{isAudioMuted ? "Unmute" : "Mute"}</Text>
                </TouchableOpacity>

                <TouchableOpacity style={[styles.controlButton, isVideoMuted && styles.mutedButton]} onPress={toggleVideo}>
                  <Text style={styles.buttonText}>{isVideoMuted ? "Video On" : "Video Off"}</Text>
                </TouchableOpacity>

                <TouchableOpacity style={styles.controlButton} onPress={() => WebRTCService.switchCamera()}>
                  <Text style={styles.buttonText}>Flip</Text>
                </TouchableOpacity>

                <TouchableOpacity style={styles.hangupButton} onPress={handleHangup}>
                  <Text style={styles.buttonText}>Hangup</Text>
                </TouchableOpacity>
              </View>
            )}
          </View>
        )}
      </View>
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#000",
  },
  remoteVideo: {
    flex: 1,
  },
  localVideo: {
    position: "absolute",
    top: 50,
    right: 20,
    width: 120,
    height: 160,
    borderRadius: 8,
    overflow: "hidden",
  },
  controls: {
    position: "absolute",
    bottom: 50,
    left: 20,
    right: 20,
  },
  registerForm: {
    backgroundColor: "rgba(0,0,0,0.7)",
    padding: 20,
    borderRadius: 12,
  },
  callControls: {
    backgroundColor: "rgba(0,0,0,0.7)",
    padding: 20,
    borderRadius: 12,
  },
  input: {
    backgroundColor: "#fff",
    padding: 12,
    borderRadius: 8,
    marginBottom: 10,
    fontSize: 16,
  },
  button: {
    backgroundColor: "#4CAF50",
    padding: 15,
    borderRadius: 8,
    alignItems: "center",
  },
  callButton: {
    backgroundColor: "#4CAF50",
    padding: 15,
    borderRadius: 8,
    alignItems: "center",
  },
  hangupButton: {
    backgroundColor: "#F44336",
    padding: 15,
    borderRadius: 8,
    alignItems: "center",
    flex: 1,
  },
  controlButton: {
    backgroundColor: "#2196F3",
    padding: 15,
    borderRadius: 8,
    alignItems: "center",
    flex: 1,
    marginHorizontal: 5,
  },
  mutedButton: {
    backgroundColor: "#FF9800",
  },
  buttonText: {
    color: "#fff",
    fontSize: 16,
    fontWeight: "bold",
  },
  stateText: {
    color: "#fff",
    fontSize: 14,
    marginBottom: 10,
    textAlign: "center",
  },
  inCallButtons: {
    flexDirection: "row",
    flexWrap: "wrap",
    justifyContent: "center",
    gap: 10,
  },
});

export default VideoCallScreen;
```

---

## SIP MESSAGE (Text Messaging)

### Send/Receive Messages

```tsx
// ส่งข้อความ (ระหว่างโทร)
WebRTCService.sendMessage("Hello from React Native!");

// รับข้อความ
WebRTCService.onMessage = (from: string, body: string) => {
  console.log(`Message from ${from}: ${body}`);
  // แสดง notification หรือ update UI
};
```

### Message Component Example

```tsx
// src/components/MessagePanel.tsx
import React, { useState } from "react";
import { View, TextInput, TouchableOpacity, Text, FlatList, StyleSheet } from "react-native";
import WebRTCService from "../services/WebRTCService";

interface Message {
  id: string;
  from: string;
  body: string;
  isOutgoing: boolean;
  timestamp: Date;
}

const MessagePanel: React.FC = () => {
  const [messages, setMessages] = useState<Message[]>([]);
  const [inputText, setInputText] = useState("");

  // Setup message listener
  React.useEffect(() => {
    WebRTCService.onMessage = (from, body) => {
      setMessages((prev) => [
        ...prev,
        {
          id: Date.now().toString(),
          from,
          body,
          isOutgoing: false,
          timestamp: new Date(),
        },
      ]);
    };
  }, []);

  const handleSend = () => {
    if (!inputText.trim()) return;

    // Send message
    WebRTCService.sendMessage(inputText);

    // Add to local list
    setMessages((prev) => [
      ...prev,
      {
        id: Date.now().toString(),
        from: "You",
        body: inputText,
        isOutgoing: true,
        timestamp: new Date(),
      },
    ]);

    setInputText("");
  };

  return (
    <View style={styles.container}>
      <FlatList
        data={messages}
        keyExtractor={(item) => item.id}
        renderItem={({ item }) => (
          <View style={[styles.message, item.isOutgoing ? styles.outgoing : styles.incoming]}>
            <Text style={styles.from}>{item.from}</Text>
            <Text style={styles.body}>{item.body}</Text>
          </View>
        )}
      />

      <View style={styles.inputRow}>
        <TextInput style={styles.input} value={inputText} onChangeText={setInputText} placeholder="Type a message..." />
        <TouchableOpacity style={styles.sendButton} onPress={handleSend}>
          <Text style={styles.sendText}>Send</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
};

const styles = StyleSheet.create({
  container: { flex: 1 },
  message: { padding: 10, marginVertical: 5, marginHorizontal: 10, borderRadius: 8 },
  outgoing: { backgroundColor: "#DCF8C6", alignSelf: "flex-end" },
  incoming: { backgroundColor: "#E8E8E8", alignSelf: "flex-start" },
  from: { fontWeight: "bold", fontSize: 12, marginBottom: 4 },
  body: { fontSize: 16 },
  inputRow: { flexDirection: "row", padding: 10 },
  input: { flex: 1, backgroundColor: "#f0f0f0", padding: 12, borderRadius: 20 },
  sendButton: { backgroundColor: "#4CAF50", padding: 12, borderRadius: 20, marginLeft: 10 },
  sendText: { color: "#fff", fontWeight: "bold" },
});

export default MessagePanel;
```

---

## DTMF (Dual-Tone Multi-Frequency)

K2 Gateway รองรับ RFC 2833 DTMF สำหรับการโต้ตอบกับ IVR/PBX systems

### Send DTMF

```typescript
// ส่ง DTMF digit เดียว
WebRTCService.sendDTMF("5");

// ส่งหลาย digits
WebRTCService.sendDTMF("1234#");
```

### Receive DTMF

```typescript
// รับ DTMF จาก SIP peer
WebRTCService.onDTMF = (digit: string, sessionId: string) => {
  console.log(`DTMF received: ${digit}`);
  // เล่นเสียง DTMF หรือแสดงบน UI
};
```

### Dialpad Component Example

```tsx
// src/components/Dialpad.tsx
import React, { useState, useEffect } from "react";
import { View, TouchableOpacity, Text, StyleSheet, Animated } from "react-native";
import WebRTCService from "../services/WebRTCService";

const DTMF_KEYS = [
  ["1", "2", "3"],
  ["4", "5", "6"],
  ["7", "8", "9"],
  ["*", "0", "#"],
];

// DTMF Frequencies for audio feedback
const DTMF_FREQS: Record<string, [number, number]> = {
  "1": [697, 1209],
  "2": [697, 1336],
  "3": [697, 1477],
  "4": [770, 1209],
  "5": [770, 1336],
  "6": [770, 1477],
  "7": [852, 1209],
  "8": [852, 1336],
  "9": [852, 1477],
  "*": [941, 1209],
  "0": [941, 1336],
  "#": [941, 1477],
};

interface Props {
  onDigitPress?: (digit: string) => void;
  disabled?: boolean;
}

const Dialpad: React.FC<Props> = ({ onDigitPress, disabled = false }) => {
  const [pressedKey, setPressedKey] = useState<string | null>(null);
  const [receivedDigit, setReceivedDigit] = useState<string | null>(null);
  const flashAnim = useState(new Animated.Value(1))[0];

  useEffect(() => {
    // Listen for incoming DTMF
    WebRTCService.onDTMF = (digit) => {
      setReceivedDigit(digit);

      // Flash animation
      Animated.sequence([
        Animated.timing(flashAnim, { toValue: 0.3, duration: 100, useNativeDriver: true }),
        Animated.timing(flashAnim, { toValue: 1, duration: 100, useNativeDriver: true }),
      ]).start();

      // Clear after animation
      setTimeout(() => setReceivedDigit(null), 300);
    };

    return () => {
      WebRTCService.onDTMF = null;
    };
  }, []);

  const handlePress = (digit: string) => {
    if (disabled) return;

    setPressedKey(digit);
    setTimeout(() => setPressedKey(null), 150);

    // Send DTMF to server
    WebRTCService.sendDTMF(digit);

    // Callback for parent component
    onDigitPress?.(digit);
  };

  const getKeyStyle = (digit: string) => {
    if (digit === receivedDigit) {
      return [styles.key, styles.keyReceived];
    }
    if (digit === pressedKey) {
      return [styles.key, styles.keyPressed];
    }
    return styles.key;
  };

  return (
    <View style={styles.container}>
      {DTMF_KEYS.map((row, rowIndex) => (
        <View key={rowIndex} style={styles.row}>
          {row.map((digit) => (
            <Animated.View key={digit} style={[{ opacity: digit === receivedDigit ? flashAnim : 1 }]}>
              <TouchableOpacity style={getKeyStyle(digit)} onPress={() => handlePress(digit)} disabled={disabled} activeOpacity={0.7}>
                <Text style={styles.keyText}>{digit}</Text>
              </TouchableOpacity>
            </Animated.View>
          ))}
        </View>
      ))}

      {receivedDigit && (
        <View style={styles.receivedBadge}>
          <Text style={styles.receivedText}>Received: {receivedDigit}</Text>
        </View>
      )}
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    padding: 16,
    backgroundColor: "rgba(0, 0, 0, 0.7)",
    borderRadius: 16,
  },
  row: {
    flexDirection: "row",
    justifyContent: "center",
    marginBottom: 12,
  },
  key: {
    width: 70,
    height: 70,
    borderRadius: 35,
    backgroundColor: "rgba(255, 255, 255, 0.1)",
    justifyContent: "center",
    alignItems: "center",
    marginHorizontal: 8,
  },
  keyPressed: {
    backgroundColor: "#4CAF50",
    transform: [{ scale: 0.95 }],
  },
  keyReceived: {
    backgroundColor: "#2196F3",
    shadowColor: "#2196F3",
    shadowOffset: { width: 0, height: 0 },
    shadowOpacity: 0.8,
    shadowRadius: 10,
    elevation: 5,
  },
  keyText: {
    color: "#fff",
    fontSize: 28,
    fontWeight: "600",
  },
  receivedBadge: {
    position: "absolute",
    top: -30,
    right: 10,
    backgroundColor: "#2196F3",
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 12,
  },
  receivedText: {
    color: "#fff",
    fontSize: 14,
    fontWeight: "bold",
  },
});

export default Dialpad;
```

### Using Dialpad in Call Screen

```tsx
// In VideoCallScreen.tsx
import Dialpad from "../components/Dialpad";

// Inside render, when call is active:
{
  callState === "active" && (
    <View style={styles.dialpadContainer}>
      <Dialpad onDigitPress={(digit) => console.log(`Pressed: ${digit}`)} disabled={callState !== "active"} />
    </View>
  );
}
```

---

## WebSocket Message Types

| Direction | Type             | Description                                |
| --------- | ---------------- | ------------------------------------------ |
| C → S     | `offer`          | WebRTC SDP offer                           |
| S → C     | `answer`         | WebRTC SDP answer with sessionId           |
| C → S     | `register`       | SIP registration request                   |
| S → C     | `registerStatus` | Registration result                        |
| C → S     | `call`           | Initiate outbound call                     |
| C → S     | `hangup`         | End call                                   |
| C → S     | `accept`         | Accept incoming call                       |
| C → S     | `reject`         | Reject incoming call                       |
| S → C     | `state`          | Call state update (ringing, active, ended) |
| S → C     | `incoming`       | Incoming call notification                 |
| C → S     | `dtmf`           | Send DTMF digits                           |
| S → C     | `dtmf`           | Receive DTMF from SIP peer (RFC 2833)      |
| C → S     | `send_message`   | Send SIP MESSAGE                           |
| S → C     | `message`        | Incoming SIP MESSAGE                       |

Incoming accept flow in current API mode:
1. Receive `incoming` with SIP inbound `sessionId`
2. Ensure client has an active WebRTC session (offer/answer)
3. Send `accept` using the incoming SIP `sessionId`

Do not send a client-side incoming `answer` message; the API handler only processes `accept`/`reject` for inbound SIP calls.

> Public SIP identity guard: if a session already uses public credentials and the next `call`
> changes `sipUsername` or `sipDomain`, gateway returns `type: "error"`.
> Client must create a new session by sending a new `offer`.

---

## Troubleshooting

### iOS Issues

1. **Camera/Mic not working**: ตรวจสอบ Info.plist permissions
2. **Build errors**: `pod install` ใหม่หลัง update package

### Android Issues

1. **Permissions denied**: ใช้ `PermissionsAndroid.request()` ก่อนเรียก `getUserMedia`
2. **Proguard issues**: เพิ่ม rules ใน `proguard-rules.pro`:

```
-keep class org.webrtc.** { *; }
```

### General

1. **No remote audio/video**: ตรวจสอบ TURN server connectivity
2. **Call fails**: ตรวจสอบ SIP registration ก่อนโทร
3. **WebSocket disconnect**: ใช้ reconnection logic

---

## Additional Resources

- [react-native-webrtc](https://github.com/react-native-webrtc/react-native-webrtc)
- [K2 Gateway WebRTC Guide](../WebRTC.md)
- [WebRTC API](https://developer.mozilla.org/en-US/docs/Web/API/WebRTC_API)
