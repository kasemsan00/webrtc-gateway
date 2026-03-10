# iOS WebRTC Integration Guide

คู่มือการใช้งาน K2 Gateway กับ iOS Native (Swift) สำหรับการโทร SIP/WebRTC

## Prerequisites

### CocoaPods (Podfile)

```ruby
platform :ios, '13.0'
use_frameworks!

target 'K2Gateway' do
  pod 'WebRTC-SDK', '~> 114.5735.08'
  pod 'Starscream', '~> 4.0.6'  # WebSocket client
end
```

```bash
pod install
```

### Info.plist Permissions

```xml
<key>NSCameraUsageDescription</key>
<string>Camera access is required for video calls</string>
<key>NSMicrophoneUsageDescription</key>
<string>Microphone access is required for voice calls</string>
```

### Background Modes (Optional)

```xml
<key>UIBackgroundModes</key>
<array>
    <string>audio</string>
    <string>voip</string>
</array>
```

---

## Basic Implementation

### 1. WebRTC Service

```swift
// K2WebRTCService.swift
import Foundation
import WebRTC
import Starscream

protocol K2WebRTCServiceDelegate: AnyObject {
    func didReceiveLocalStream(videoTrack: RTCVideoTrack?, audioTrack: RTCAudioTrack?)
    func didReceiveRemoteStream(videoTrack: RTCVideoTrack?, audioTrack: RTCAudioTrack?)
    func didChangeCallState(_ state: String)
    func didReceiveMessage(from: String, body: String)
    func didReceiveIncomingCall(from: String, sessionId: String)
    func didChangeRegistrationStatus(_ registered: Bool)
    func didConnect()
    func didDisconnect()
    func didReceiveError(_ error: String)
}

class K2WebRTCService: NSObject {

    // MARK: - Properties

    weak var delegate: K2WebRTCServiceDelegate?

    private var socket: WebSocket?
    private var peerConnectionFactory: RTCPeerConnectionFactory?
    private var peerConnection: RTCPeerConnection?
    private var localVideoTrack: RTCVideoTrack?
    private var localAudioTrack: RTCAudioTrack?
    private var videoCapturer: RTCCameraVideoCapturer?

    private var sessionId: String?

    private let iceServers = [
        RTCIceServer(
            urlStrings: ["turn:turn.ttrs.or.th:3478?transport=udp"],
            username: "turn01",
            credential: "Test1234"
        )
    ]

    // MARK: - Initialization

    override init() {
        super.init()
        initializeWebRTC()
    }

    private func initializeWebRTC() {
        RTCInitializeSSL()

        let encoderFactory = RTCDefaultVideoEncoderFactory()
        let decoderFactory = RTCDefaultVideoDecoderFactory()

        peerConnectionFactory = RTCPeerConnectionFactory(
            encoderFactory: encoderFactory,
            decoderFactory: decoderFactory
        )
    }

    // MARK: - WebSocket Connection

    func connect(to url: String) {
        guard let wsURL = URL(string: url) else { return }

        var request = URLRequest(url: wsURL)
        request.timeoutInterval = 30

        socket = WebSocket(request: request)
        socket?.delegate = self
        socket?.connect()
    }

    func disconnect() {
        cleanup()
        socket?.disconnect()
        socket = nil
    }

    // MARK: - Media Session

    func startSession(localVideoView: RTCMTLVideoView?) {
        guard let factory = peerConnectionFactory else { return }

        // Create audio track
        let audioConstraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        let audioSource = factory.audioSource(with: audioConstraints)
        localAudioTrack = factory.audioTrack(with: audioSource, trackId: "audio0")

        // Create video track
        let videoSource = factory.videoSource()
        videoCapturer = RTCCameraVideoCapturer(delegate: videoSource)
        localVideoTrack = factory.videoTrack(with: videoSource, trackId: "video0")

        // Start camera capture
        startCapture()

        // Add local video to view
        if let view = localVideoView, let track = localVideoTrack {
            track.add(view)
        }

        delegate?.didReceiveLocalStream(videoTrack: localVideoTrack, audioTrack: localAudioTrack)

        // Create peer connection
        createPeerConnection()

        // Create and send offer
        createOffer()
    }

    private func startCapture() {
        guard let capturer = videoCapturer else { return }

        let devices = RTCCameraVideoCapturer.captureDevices()
        guard let frontCamera = devices.first(where: { $0.position == .front }) ?? devices.first else {
            return
        }

        let formats = RTCCameraVideoCapturer.supportedFormats(for: frontCamera)
        guard let format = formats.first(where: { format in
            let dimensions = CMVideoFormatDescriptionGetDimensions(format.formatDescription)
            return dimensions.width == 640 && dimensions.height == 480
        }) ?? formats.first else { return }

        let fps = format.videoSupportedFrameRateRanges.first?.maxFrameRate ?? 30

        capturer.startCapture(with: frontCamera, format: format, fps: Int(fps))
    }

    private func createPeerConnection() {
        guard let factory = peerConnectionFactory else { return }

        let config = RTCConfiguration()
        config.iceServers = iceServers
        config.sdpSemantics = .unifiedPlan

        let constraints = RTCMediaConstraints(
            mandatoryConstraints: nil,
            optionalConstraints: nil
        )

        peerConnection = factory.peerConnection(
            with: config,
            constraints: constraints,
            delegate: self
        )

        // Add local tracks
        if let audioTrack = localAudioTrack {
            peerConnection?.add(audioTrack, streamIds: ["stream0"])
        }
        if let videoTrack = localVideoTrack {
            peerConnection?.add(videoTrack, streamIds: ["stream0"])
        }
    }

    private func createOffer() {
        let constraints = RTCMediaConstraints(
            mandatoryConstraints: [
                "OfferToReceiveAudio": "true",
                "OfferToReceiveVideo": "true"
            ],
            optionalConstraints: nil
        )

        peerConnection?.offer(for: constraints) { [weak self] sdp, error in
            guard let self = self, let sdp = sdp, error == nil else {
                self?.delegate?.didReceiveError(error?.localizedDescription ?? "Failed to create offer")
                return
            }

            self.peerConnection?.setLocalDescription(sdp) { error in
                if let error = error {
                    self.delegate?.didReceiveError(error.localizedDescription)
                    return
                }

                // Wait for ICE gathering then send offer
                DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                    self.sendOffer(sdp.sdp)
                }
            }
        }
    }

    private func sendOffer(_ sdp: String) {
        let message: [String: Any] = [
            "type": "offer",
            "sdp": sdp
        ]
        send(message)
    }

    private func handleAnswer(_ data: [String: Any]) {
        guard let sdpString = data["sdp"] as? String else { return }

        sessionId = data["sessionId"] as? String

        let answer = RTCSessionDescription(type: .answer, sdp: sdpString)
        peerConnection?.setRemoteDescription(answer) { [weak self] error in
            if let error = error {
                self?.delegate?.didReceiveError(error.localizedDescription)
            }
        }
    }

    // MARK: - SIP Registration

    func register(domain: String, username: String, password: String, port: Int = 5060) {
        let message: [String: Any] = [
            "type": "register",
            "sipDomain": domain,
            "sipUsername": username,
            "sipPassword": password,
            "sipPort": port
        ]
        send(message)
    }

    func unregister() {
        send(["type": "unregister"])
    }

    // MARK: - Call Controls

    func call(destination: String) {
        guard let sid = sessionId else { return }

        let message: [String: Any] = [
            "type": "call",
            "sessionId": sid,
            "destination": destination
        ]
        send(message)
    }

    func hangup() {
        guard let sid = sessionId else { return }

        let message: [String: Any] = [
            "type": "hangup",
            "sessionId": sid
        ]
        send(message)
    }

    func acceptCall(sessionId: String) {
        let message: [String: Any] = [
            "type": "accept",
            "sessionId": sessionId
        ]
        send(message)
    }

    func rejectCall(sessionId: String) {
        let message: [String: Any] = [
            "type": "reject",
            "sessionId": sessionId
        ]
        send(message)
    }

    // MARK: - DTMF

    func sendDTMF(digits: String) {
        guard let sid = sessionId else { return }

        let message: [String: Any] = [
            "type": "dtmf",
            "sessionId": sid,
            "digits": digits
        ]
        send(message)
    }

    // MARK: - SIP MESSAGE

    func sendMessage(body: String, contentType: String = "text/plain;charset=UTF-8") {
        let message: [String: Any] = [
            "type": "send_message",
            "body": body,
            "contentType": contentType
        ]
        send(message)
    }

    // MARK: - Mute Controls

    func toggleAudioMute() -> Bool {
        guard let track = localAudioTrack else { return false }
        track.isEnabled = !track.isEnabled
        return !track.isEnabled
    }

    func toggleVideoMute() -> Bool {
        guard let track = localVideoTrack else { return false }
        track.isEnabled = !track.isEnabled
        return !track.isEnabled
    }

    // MARK: - Camera Switch

    func switchCamera() {
        guard let capturer = videoCapturer else { return }

        capturer.stopCapture()

        let devices = RTCCameraVideoCapturer.captureDevices()
        let currentPosition = capturer.captureSession.inputs
            .compactMap { ($0 as? AVCaptureDeviceInput)?.device.position }
            .first ?? .front

        let newPosition: AVCaptureDevice.Position = currentPosition == .front ? .back : .front

        guard let newDevice = devices.first(where: { $0.position == newPosition }) ?? devices.first else {
            return
        }

        let formats = RTCCameraVideoCapturer.supportedFormats(for: newDevice)
        guard let format = formats.first(where: { format in
            let dimensions = CMVideoFormatDescriptionGetDimensions(format.formatDescription)
            return dimensions.width == 640 && dimensions.height == 480
        }) ?? formats.first else { return }

        let fps = format.videoSupportedFrameRateRanges.first?.maxFrameRate ?? 30

        capturer.startCapture(with: newDevice, format: format, fps: Int(fps))
    }

    // MARK: - Helpers

    private func send(_ message: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: message),
              let text = String(data: data, encoding: .utf8) else { return }
        socket?.write(string: text)
    }

    private func cleanup() {
        videoCapturer?.stopCapture()
        localVideoTrack = nil
        localAudioTrack = nil
        peerConnection?.close()
        peerConnection = nil
        sessionId = nil
    }
}

// MARK: - WebSocketDelegate

extension K2WebRTCService: WebSocketDelegate {

    func didReceive(event: WebSocketEvent, client: WebSocketClient) {
        switch event {
        case .connected(_):
            delegate?.didConnect()

        case .disconnected(_, _):
            delegate?.didDisconnect()

        case .text(let text):
            handleMessage(text)

        case .error(let error):
            delegate?.didReceiveError(error?.localizedDescription ?? "Unknown error")

        default:
            break
        }
    }

    private func handleMessage(_ text: String) {
        guard let data = text.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = json["type"] as? String else { return }

        DispatchQueue.main.async { [weak self] in
            switch type {
            case "answer":
                self?.handleAnswer(json)

            case "state":
                if let sid = json["sessionId"] as? String {
                    self?.sessionId = sid
                }
                if let state = json["state"] as? String {
                    self?.delegate?.didChangeCallState(state)
                }

            case "incoming":
                let from = json["from"] as? String ?? "Unknown"
                let sid = json["sessionId"] as? String ?? ""
                self?.delegate?.didReceiveIncomingCall(from: from, sessionId: sid)

            case "registerStatus":
                let registered = json["registered"] as? Bool ?? false
                self?.delegate?.didChangeRegistrationStatus(registered)

            case "message":
                let from = json["from"] as? String ?? ""
                let body = json["body"] as? String ?? ""
                self?.delegate?.didReceiveMessage(from: from, body: body)

            case "error":
                let error = json["error"] as? String ?? "Unknown error"
                self?.delegate?.didReceiveError(error)

            default:
                break
            }
        }
    }
}

// MARK: - RTCPeerConnectionDelegate

extension K2WebRTCService: RTCPeerConnectionDelegate {

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {
        print("Signaling state: \(stateChanged)")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {
        print("Stream added")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {
        print("Stream removed")
    }

    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {
        print("Renegotiation needed")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        print("ICE connection state: \(newState)")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {
        print("ICE gathering state: \(newState)")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {
        print("ICE candidate generated")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {
        print("ICE candidates removed")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {
        print("Data channel opened")
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd rtpReceiver: RTCRtpReceiver, streams mediaStreams: [RTCMediaStream]) {
        print("RTP receiver added: \(rtpReceiver.track?.kind ?? "unknown")")

        if let videoTrack = rtpReceiver.track as? RTCVideoTrack {
            delegate?.didReceiveRemoteStream(videoTrack: videoTrack, audioTrack: nil)
        } else if let audioTrack = rtpReceiver.track as? RTCAudioTrack {
            delegate?.didReceiveRemoteStream(videoTrack: nil, audioTrack: audioTrack)
        }
    }
}
```

---

## Reconnection Support

### K2WebRTCService with Auto Reconnect

```swift
// K2WebRTCServiceWithReconnect.swift
import Foundation
import WebRTC
import Starscream

// MARK: - Reconnection Configuration

struct ReconnectConfig {
    var maxAttempts: Int = 5              // Maximum retry attempts
    var baseDelay: TimeInterval = 1.0     // Initial delay in seconds
    var maxDelay: TimeInterval = 30.0     // Maximum delay in seconds
    var backoffMultiplier: Double = 2.0   // Exponential backoff multiplier
}

enum ConnectionState: String {
    case disconnected
    case connecting
    case connected
    case reconnecting
}

// MARK: - Extended Delegate

protocol K2WebRTCServiceDelegate: AnyObject {
    func didReceiveLocalStream(videoTrack: RTCVideoTrack?, audioTrack: RTCAudioTrack?)
    func didReceiveRemoteStream(videoTrack: RTCVideoTrack?, audioTrack: RTCAudioTrack?)
    func didChangeCallState(_ state: String)
    func didReceiveMessage(from: String, body: String)
    func didReceiveIncomingCall(from: String, sessionId: String)
    func didChangeRegistrationStatus(_ registered: Bool)
    func didConnect()
    func didDisconnect()
    func didReceiveError(_ error: String)

    // Reconnection callbacks
    func didChangeConnectionState(_ state: ConnectionState)
    func didStartReconnecting(attempt: Int, maxAttempts: Int)
    func didFailToReconnect()
}

// Default implementation for optional methods
extension K2WebRTCServiceDelegate {
    func didChangeConnectionState(_ state: ConnectionState) {}
    func didStartReconnecting(attempt: Int, maxAttempts: Int) {}
    func didFailToReconnect() {}
}

// MARK: - Service with Reconnection

class K2WebRTCService: NSObject {

    // MARK: - Properties

    weak var delegate: K2WebRTCServiceDelegate?

    private var socket: WebSocket?
    private var peerConnectionFactory: RTCPeerConnectionFactory?
    private var peerConnection: RTCPeerConnection?
    private var localVideoTrack: RTCVideoTrack?
    private var localAudioTrack: RTCAudioTrack?
    private var videoCapturer: RTCCameraVideoCapturer?

    private var sessionId: String?

    private let iceServers = [
        RTCIceServer(
            urlStrings: ["turn:turn.ttrs.or.th:3478?transport=udp"],
            username: "turn01",
            credential: "Test1234"
        )
    ]

    // Reconnection state
    private var wsUrl: String = ""
    private var reconnectAttempts: Int = 0
    private var reconnectTimer: Timer?
    private var isManualDisconnect: Bool = false
    private(set) var connectionState: ConnectionState = .disconnected {
        didSet {
            delegate?.didChangeConnectionState(connectionState)
        }
    }

    var reconnectConfig = ReconnectConfig()

    // SIP credentials for re-registration
    private var sipCredentials: (domain: String, username: String, password: String, port: Int)?

    // MARK: - Initialization

    override init() {
        super.init()
        initializeWebRTC()
    }

    private func initializeWebRTC() {
        RTCInitializeSSL()

        let encoderFactory = RTCDefaultVideoEncoderFactory()
        let decoderFactory = RTCDefaultVideoDecoderFactory()

        peerConnectionFactory = RTCPeerConnectionFactory(
            encoderFactory: encoderFactory,
            decoderFactory: decoderFactory
        )
    }

    // MARK: - Reconnection Logic

    private func getReconnectDelay() -> TimeInterval {
        let delay = min(
            reconnectConfig.baseDelay * pow(reconnectConfig.backoffMultiplier, Double(reconnectAttempts)),
            reconnectConfig.maxDelay
        )
        // Add jitter (±20%) to prevent thundering herd
        let jitter = delay * 0.2 * (Double.random(in: -1...1))
        return delay + jitter
    }

    private func scheduleReconnect() {
        guard reconnectAttempts < reconnectConfig.maxAttempts else {
            print("Max reconnect attempts reached")
            delegate?.didFailToReconnect()
            return
        }

        let delay = getReconnectDelay()
        reconnectAttempts += 1

        print("Reconnecting in \(delay)s (attempt \(reconnectAttempts)/\(reconnectConfig.maxAttempts))")
        delegate?.didStartReconnecting(attempt: reconnectAttempts, maxAttempts: reconnectConfig.maxAttempts)

        reconnectTimer = Timer.scheduledTimer(withTimeInterval: delay, repeats: false) { [weak self] _ in
            self?.establishConnection()
        }
    }

    func cancelReconnect() {
        reconnectTimer?.invalidate()
        reconnectTimer = nil
        reconnectAttempts = 0
    }

    /// Manual reconnect
    func reconnect() {
        cancelReconnect()
        cleanup()
        reconnectAttempts = 0
        isManualDisconnect = false
        establishConnection()
    }

    // MARK: - WebSocket Connection

    func connect(to url: String) {
        wsUrl = url
        isManualDisconnect = false
        reconnectAttempts = 0
        establishConnection()
    }

    private func establishConnection() {
        guard let wsURL = URL(string: wsUrl) else { return }

        connectionState = reconnectAttempts > 0 ? .reconnecting : .connecting

        var request = URLRequest(url: wsURL)
        request.timeoutInterval = 30

        socket = WebSocket(request: request)
        socket?.delegate = self
        socket?.connect()
    }

    func disconnect() {
        isManualDisconnect = true
        cancelReconnect()
        cleanup()
        sipCredentials = nil
        socket?.disconnect()
        socket = nil
        connectionState = .disconnected
    }

    // MARK: - Media Session

    func startSession(localVideoView: RTCMTLVideoView?) {
        guard let factory = peerConnectionFactory else { return }

        // Create audio track
        let audioConstraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        let audioSource = factory.audioSource(with: audioConstraints)
        localAudioTrack = factory.audioTrack(with: audioSource, trackId: "audio0")

        // Create video track
        let videoSource = factory.videoSource()
        videoCapturer = RTCCameraVideoCapturer(delegate: videoSource)
        localVideoTrack = factory.videoTrack(with: videoSource, trackId: "video0")

        // Start camera capture
        startCapture()

        // Add local video to view
        if let view = localVideoView, let track = localVideoTrack {
            track.add(view)
        }

        delegate?.didReceiveLocalStream(videoTrack: localVideoTrack, audioTrack: localAudioTrack)

        // Create peer connection
        createPeerConnection()

        // Create and send offer
        createOffer()
    }

    private func startCapture() {
        guard let capturer = videoCapturer else { return }

        let devices = RTCCameraVideoCapturer.captureDevices()
        guard let frontCamera = devices.first(where: { $0.position == .front }) ?? devices.first else {
            return
        }

        let formats = RTCCameraVideoCapturer.supportedFormats(for: frontCamera)
        guard let format = formats.first(where: { format in
            let dimensions = CMVideoFormatDescriptionGetDimensions(format.formatDescription)
            return dimensions.width == 640 && dimensions.height == 480
        }) ?? formats.first else { return }

        let fps = format.videoSupportedFrameRateRanges.first?.maxFrameRate ?? 30

        capturer.startCapture(with: frontCamera, format: format, fps: Int(fps))
    }

    private func createPeerConnection() {
        guard let factory = peerConnectionFactory else { return }

        let config = RTCConfiguration()
        config.iceServers = iceServers
        config.sdpSemantics = .unifiedPlan

        let constraints = RTCMediaConstraints(
            mandatoryConstraints: nil,
            optionalConstraints: nil
        )

        peerConnection = factory.peerConnection(
            with: config,
            constraints: constraints,
            delegate: self
        )

        // Add local tracks
        if let audioTrack = localAudioTrack {
            peerConnection?.add(audioTrack, streamIds: ["stream0"])
        }
        if let videoTrack = localVideoTrack {
            peerConnection?.add(videoTrack, streamIds: ["stream0"])
        }
    }

    private func createOffer() {
        let constraints = RTCMediaConstraints(
            mandatoryConstraints: [
                "OfferToReceiveAudio": "true",
                "OfferToReceiveVideo": "true"
            ],
            optionalConstraints: nil
        )

        peerConnection?.offer(for: constraints) { [weak self] sdp, error in
            guard let self = self, let sdp = sdp, error == nil else {
                self?.delegate?.didReceiveError(error?.localizedDescription ?? "Failed to create offer")
                return
            }

            self.peerConnection?.setLocalDescription(sdp) { error in
                if let error = error {
                    self.delegate?.didReceiveError(error.localizedDescription)
                    return
                }

                // Wait for ICE gathering then send offer
                DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                    self.sendOffer(sdp.sdp)
                }
            }
        }
    }

    private func sendOffer(_ sdp: String) {
        let message: [String: Any] = [
            "type": "offer",
            "sdp": sdp
        ]
        send(message)
    }

    private func handleAnswer(_ data: [String: Any]) {
        guard let sdpString = data["sdp"] as? String else { return }

        sessionId = data["sessionId"] as? String

        let answer = RTCSessionDescription(type: .answer, sdp: sdpString)
        peerConnection?.setRemoteDescription(answer) { [weak self] error in
            if let error = error {
                self?.delegate?.didReceiveError(error.localizedDescription)
            }
        }
    }

    // MARK: - SIP Registration (stores credentials)

    func register(domain: String, username: String, password: String, port: Int = 5060) {
        // Store credentials for reconnection
        sipCredentials = (domain, username, password, port)

        let message: [String: Any] = [
            "type": "register",
            "sipDomain": domain,
            "sipUsername": username,
            "sipPassword": password,
            "sipPort": port
        ]
        send(message)
    }

    func unregister() {
        sipCredentials = nil
        send(["type": "unregister"])
    }

    // MARK: - Call Controls

    func call(destination: String) {
        guard let sid = sessionId else { return }

        let message: [String: Any] = [
            "type": "call",
            "sessionId": sid,
            "destination": destination
        ]
        send(message)
    }

    func hangup() {
        guard let sid = sessionId else { return }

        let message: [String: Any] = [
            "type": "hangup",
            "sessionId": sid
        ]
        send(message)
    }

    func acceptCall(sessionId: String) {
        let message: [String: Any] = [
            "type": "accept",
            "sessionId": sessionId
        ]
        send(message)
    }

    func rejectCall(sessionId: String) {
        let message: [String: Any] = [
            "type": "reject",
            "sessionId": sessionId
        ]
        send(message)
    }

    // MARK: - DTMF

    func sendDTMF(digits: String) {
        guard let sid = sessionId else { return }

        let message: [String: Any] = [
            "type": "dtmf",
            "sessionId": sid,
            "digits": digits
        ]
        send(message)
    }

    // MARK: - SIP MESSAGE

    func sendMessage(body: String, contentType: String = "text/plain;charset=UTF-8") {
        let message: [String: Any] = [
            "type": "send_message",
            "body": body,
            "contentType": contentType
        ]
        send(message)
    }

    // MARK: - Mute Controls

    func toggleAudioMute() -> Bool {
        guard let track = localAudioTrack else { return false }
        track.isEnabled = !track.isEnabled
        return !track.isEnabled
    }

    func toggleVideoMute() -> Bool {
        guard let track = localVideoTrack else { return false }
        track.isEnabled = !track.isEnabled
        return !track.isEnabled
    }

    // MARK: - Camera Switch

    func switchCamera() {
        guard let capturer = videoCapturer else { return }

        capturer.stopCapture()

        let devices = RTCCameraVideoCapturer.captureDevices()
        let currentPosition = capturer.captureSession.inputs
            .compactMap { ($0 as? AVCaptureDeviceInput)?.device.position }
            .first ?? .front

        let newPosition: AVCaptureDevice.Position = currentPosition == .front ? .back : .front

        guard let newDevice = devices.first(where: { $0.position == newPosition }) ?? devices.first else {
            return
        }

        let formats = RTCCameraVideoCapturer.supportedFormats(for: newDevice)
        guard let format = formats.first(where: { format in
            let dimensions = CMVideoFormatDescriptionGetDimensions(format.formatDescription)
            return dimensions.width == 640 && dimensions.height == 480
        }) ?? formats.first else { return }

        let fps = format.videoSupportedFrameRateRanges.first?.maxFrameRate ?? 30

        capturer.startCapture(with: newDevice, format: format, fps: Int(fps))
    }

    // MARK: - Helpers

    private func send(_ message: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: message),
              let text = String(data: data, encoding: .utf8) else { return }

        if socket?.isConnected == true {
            socket?.write(string: text)
        } else {
            print("WebSocket not connected, message not sent: \(message["type"] ?? "")")
        }
    }

    private func cleanup() {
        videoCapturer?.stopCapture()
        localVideoTrack = nil
        localAudioTrack = nil
        peerConnection?.close()
        peerConnection = nil
        sessionId = nil
    }
}

// MARK: - WebSocketDelegate

extension K2WebRTCService: WebSocketDelegate {

    func didReceive(event: WebSocketEvent, client: WebSocketClient) {
        switch event {
        case .connected(_):
            print("WebSocket connected")
            reconnectAttempts = 0
            connectionState = .connected
            delegate?.didConnect()

            // Re-register if we have stored credentials
            if let creds = sipCredentials {
                print("Re-registering after reconnect...")
                register(domain: creds.domain, username: creds.username, password: creds.password, port: creds.port)
            }

        case .disconnected(_, _):
            print("WebSocket disconnected")
            connectionState = .disconnected
            delegate?.didDisconnect()

            if !isManualDisconnect {
                scheduleReconnect()
            }

        case .text(let text):
            handleMessage(text)

        case .error(let error):
            delegate?.didReceiveError(error?.localizedDescription ?? "Unknown error")

            if !isManualDisconnect {
                scheduleReconnect()
            }

        default:
            break
        }
    }

    private func handleMessage(_ text: String) {
        guard let data = text.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = json["type"] as? String else { return }

        DispatchQueue.main.async { [weak self] in
            switch type {
            case "answer":
                self?.handleAnswer(json)

            case "state":
                if let sid = json["sessionId"] as? String {
                    self?.sessionId = sid
                }
                if let state = json["state"] as? String {
                    self?.delegate?.didChangeCallState(state)
                }

            case "incoming":
                let from = json["from"] as? String ?? "Unknown"
                let sid = json["sessionId"] as? String ?? ""
                self?.delegate?.didReceiveIncomingCall(from: from, sessionId: sid)

            case "registerStatus":
                let registered = json["registered"] as? Bool ?? false
                self?.delegate?.didChangeRegistrationStatus(registered)

            case "message":
                let from = json["from"] as? String ?? ""
                let body = json["body"] as? String ?? ""
                self?.delegate?.didReceiveMessage(from: from, body: body)

            case "error":
                let error = json["error"] as? String ?? "Unknown error"
                self?.delegate?.didReceiveError(error)

            default:
                break
            }
        }
    }
}

// MARK: - RTCPeerConnectionDelegate

extension K2WebRTCService: RTCPeerConnectionDelegate {

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}
    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}
    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd rtpReceiver: RTCRtpReceiver, streams mediaStreams: [RTCMediaStream]) {
        if let videoTrack = rtpReceiver.track as? RTCVideoTrack {
            delegate?.didReceiveRemoteStream(videoTrack: videoTrack, audioTrack: nil)
        } else if let audioTrack = rtpReceiver.track as? RTCAudioTrack {
            delegate?.didReceiveRemoteStream(videoTrack: nil, audioTrack: audioTrack)
        }
    }
}
```

### Using Reconnection in ViewController

```swift
// VideoCallViewController+Reconnect.swift

extension VideoCallViewController {

    func setupReconnection() {
        // Configure reconnection
        webRTCService.reconnectConfig = ReconnectConfig(
            maxAttempts: 5,
            baseDelay: 1.0,
            maxDelay: 30.0,
            backoffMultiplier: 2.0
        )
    }
}

// Implement reconnection delegate methods
extension VideoCallViewController: K2WebRTCServiceDelegate {

    func didChangeConnectionState(_ state: ConnectionState) {
        DispatchQueue.main.async {
            switch state {
            case .disconnected:
                self.statusLabel.text = "Disconnected"
                self.statusLabel.backgroundColor = .systemRed
            case .connecting:
                self.statusLabel.text = "Connecting..."
                self.statusLabel.backgroundColor = .systemYellow
            case .connected:
                self.statusLabel.text = "Connected"
                self.statusLabel.backgroundColor = .systemGreen
            case .reconnecting:
                self.statusLabel.text = "Reconnecting..."
                self.statusLabel.backgroundColor = .systemOrange
            }
        }
    }

    func didStartReconnecting(attempt: Int, maxAttempts: Int) {
        DispatchQueue.main.async {
            self.statusLabel.text = "Reconnecting (\(attempt)/\(maxAttempts))..."
        }
    }

    func didFailToReconnect() {
        DispatchQueue.main.async {
            let alert = UIAlertController(
                title: "Connection Lost",
                message: "Unable to reconnect to server. Would you like to try again?",
                preferredStyle: .alert
            )

            alert.addAction(UIAlertAction(title: "Retry", style: .default) { [weak self] _ in
                self?.webRTCService.reconnect()
            })

            alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))

            self.present(alert, animated: true)
        }
    }

    // ... other delegate methods
}
```

---

## ViewController Example

### 2. VideoCallViewController

```swift
// VideoCallViewController.swift
import UIKit
import WebRTC
import AVFoundation

class VideoCallViewController: UIViewController {

    // MARK: - Constants

    private let k2GatewayURL = "wss://k2-gateway.example.com/ws"

    // MARK: - UI Elements

    private lazy var remoteVideoView: RTCMTLVideoView = {
        let view = RTCMTLVideoView()
        view.videoContentMode = .scaleAspectFit
        view.translatesAutoresizingMaskIntoConstraints = false
        return view
    }()

    private lazy var localVideoView: RTCMTLVideoView = {
        let view = RTCMTLVideoView()
        view.videoContentMode = .scaleAspectFill
        view.transform = CGAffineTransform(scaleX: -1, y: 1) // Mirror
        view.translatesAutoresizingMaskIntoConstraints = false
        view.layer.cornerRadius = 8
        view.clipsToBounds = true
        return view
    }()

    private lazy var statusLabel: UILabel = {
        let label = UILabel()
        label.text = "Ready"
        label.textColor = .white
        label.textAlignment = .center
        label.backgroundColor = UIColor.black.withAlphaComponent(0.5)
        label.translatesAutoresizingMaskIntoConstraints = false
        return label
    }()

    private lazy var connectButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Connect", for: .normal)
        button.backgroundColor = .systemBlue
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(connectTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        return button
    }()

    private lazy var callButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Call", for: .normal)
        button.backgroundColor = .systemGreen
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(callTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        button.isEnabled = false
        return button
    }()

    private lazy var hangupButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Hangup", for: .normal)
        button.backgroundColor = .systemRed
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(hangupTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        button.isEnabled = false
        return button
    }()

    private lazy var muteButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Mute", for: .normal)
        button.backgroundColor = .systemGray
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(muteTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        return button
    }()

    private lazy var videoToggleButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Video Off", for: .normal)
        button.backgroundColor = .systemGray
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(videoToggleTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        return button
    }()

    private lazy var switchCameraButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Flip", for: .normal)
        button.backgroundColor = .systemGray
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(switchCameraTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        return button
    }()

    private lazy var destinationTextField: UITextField = {
        let tf = UITextField()
        tf.placeholder = "Destination (e.g. 9999)"
        tf.text = "9999"
        tf.borderStyle = .roundedRect
        tf.keyboardType = .phonePad
        tf.translatesAutoresizingMaskIntoConstraints = false
        return tf
    }()

    private lazy var sipDomainTextField: UITextField = {
        let tf = UITextField()
        tf.placeholder = "SIP Domain"
        tf.text = "sipclient.ttrs.or.th"
        tf.borderStyle = .roundedRect
        tf.translatesAutoresizingMaskIntoConstraints = false
        return tf
    }()

    private lazy var sipUsernameTextField: UITextField = {
        let tf = UITextField()
        tf.placeholder = "Username"
        tf.borderStyle = .roundedRect
        tf.translatesAutoresizingMaskIntoConstraints = false
        return tf
    }()

    private lazy var sipPasswordTextField: UITextField = {
        let tf = UITextField()
        tf.placeholder = "Password"
        tf.isSecureTextEntry = true
        tf.borderStyle = .roundedRect
        tf.translatesAutoresizingMaskIntoConstraints = false
        return tf
    }()

    private lazy var registerButton: UIButton = {
        let button = UIButton(type: .system)
        button.setTitle("Register", for: .normal)
        button.backgroundColor = .systemBlue
        button.setTitleColor(.white, for: .normal)
        button.layer.cornerRadius = 8
        button.addTarget(self, action: #selector(registerTapped), for: .touchUpInside)
        button.translatesAutoresizingMaskIntoConstraints = false
        return button
    }()

    private lazy var registrationStack: UIStackView = {
        let stack = UIStackView(arrangedSubviews: [
            sipDomainTextField, sipUsernameTextField, sipPasswordTextField, registerButton
        ])
        stack.axis = .vertical
        stack.spacing = 8
        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.isHidden = true
        return stack
    }()

    private lazy var callStack: UIStackView = {
        let stack = UIStackView(arrangedSubviews: [
            destinationTextField, callButton, hangupButton
        ])
        stack.axis = .vertical
        stack.spacing = 8
        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.isHidden = true
        return stack
    }()

    private lazy var controlStack: UIStackView = {
        let stack = UIStackView(arrangedSubviews: [
            muteButton, videoToggleButton, switchCameraButton
        ])
        stack.axis = .horizontal
        stack.spacing = 8
        stack.distribution = .fillEqually
        stack.translatesAutoresizingMaskIntoConstraints = false
        return stack
    }()

    // MARK: - Properties

    private let webRTCService = K2WebRTCService()
    private var isAudioMuted = false
    private var isVideoMuted = false

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        setupUI()
        setupWebRTC()
        requestPermissions()
    }

    private func setupUI() {
        view.backgroundColor = .black

        view.addSubview(remoteVideoView)
        view.addSubview(localVideoView)
        view.addSubview(statusLabel)
        view.addSubview(connectButton)
        view.addSubview(registrationStack)
        view.addSubview(callStack)
        view.addSubview(controlStack)

        NSLayoutConstraint.activate([
            // Remote video (full screen)
            remoteVideoView.topAnchor.constraint(equalTo: view.topAnchor),
            remoteVideoView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            remoteVideoView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            remoteVideoView.bottomAnchor.constraint(equalTo: view.bottomAnchor),

            // Local video (PIP)
            localVideoView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 16),
            localVideoView.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            localVideoView.widthAnchor.constraint(equalToConstant: 120),
            localVideoView.heightAnchor.constraint(equalToConstant: 160),

            // Status label
            statusLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 16),
            statusLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            statusLabel.heightAnchor.constraint(equalToConstant: 30),

            // Connect button
            connectButton.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            connectButton.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            connectButton.widthAnchor.constraint(equalToConstant: 150),
            connectButton.heightAnchor.constraint(equalToConstant: 44),

            // Registration stack
            registrationStack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            registrationStack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            registrationStack.bottomAnchor.constraint(equalTo: controlStack.topAnchor, constant: -16),

            // Call stack
            callStack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            callStack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            callStack.bottomAnchor.constraint(equalTo: controlStack.topAnchor, constant: -16),

            // Control stack
            controlStack.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            controlStack.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            controlStack.bottomAnchor.constraint(equalTo: view.safeAreaLayoutGuide.bottomAnchor, constant: -16),
            controlStack.heightAnchor.constraint(equalToConstant: 44),
        ])
    }

    private func setupWebRTC() {
        webRTCService.delegate = self
    }

    private func requestPermissions() {
        AVCaptureDevice.requestAccess(for: .video) { _ in }
        AVCaptureDevice.requestAccess(for: .audio) { _ in }
    }

    // MARK: - Actions

    @objc private func connectTapped() {
        connectButton.isEnabled = false
        statusLabel.text = "Connecting..."
        webRTCService.connect(to: k2GatewayURL)
    }

    @objc private func registerTapped() {
        guard let domain = sipDomainTextField.text, !domain.isEmpty,
              let username = sipUsernameTextField.text, !username.isEmpty,
              let password = sipPasswordTextField.text, !password.isEmpty else {
            showAlert(title: "Error", message: "Please fill all fields")
            return
        }

        webRTCService.register(domain: domain, username: username, password: password)
    }

    @objc private func callTapped() {
        guard let destination = destinationTextField.text, !destination.isEmpty else {
            showAlert(title: "Error", message: "Enter destination")
            return
        }

        webRTCService.call(destination: destination)
    }

    @objc private func hangupTapped() {
        webRTCService.hangup()
    }

    @objc private func muteTapped() {
        isAudioMuted = webRTCService.toggleAudioMute()
        muteButton.setTitle(isAudioMuted ? "Unmute" : "Mute", for: .normal)
        muteButton.backgroundColor = isAudioMuted ? .systemOrange : .systemGray
    }

    @objc private func videoToggleTapped() {
        isVideoMuted = webRTCService.toggleVideoMute()
        videoToggleButton.setTitle(isVideoMuted ? "Video On" : "Video Off", for: .normal)
        videoToggleButton.backgroundColor = isVideoMuted ? .systemOrange : .systemGray
    }

    @objc private func switchCameraTapped() {
        webRTCService.switchCamera()
    }

    // MARK: - Helpers

    private func showAlert(title: String, message: String) {
        let alert = UIAlertController(title: title, message: message, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "OK", style: .default))
        present(alert, animated: true)
    }

    private func showIncomingCallAlert(from: String, sessionId: String) {
        let alert = UIAlertController(
            title: "Incoming Call",
            message: "Call from: \(from)",
            preferredStyle: .alert
        )

        alert.addAction(UIAlertAction(title: "Reject", style: .destructive) { [weak self] _ in
            self?.webRTCService.rejectCall(sessionId: sessionId)
        })

        alert.addAction(UIAlertAction(title: "Accept", style: .default) { [weak self] _ in
            self?.webRTCService.acceptCall(sessionId: sessionId)
        })

        present(alert, animated: true)
    }
}

// MARK: - K2WebRTCServiceDelegate

extension VideoCallViewController: K2WebRTCServiceDelegate {

    func didReceiveLocalStream(videoTrack: RTCVideoTrack?, audioTrack: RTCAudioTrack?) {
        videoTrack?.add(localVideoView)
    }

    func didReceiveRemoteStream(videoTrack: RTCVideoTrack?, audioTrack: RTCAudioTrack?) {
        videoTrack?.add(remoteVideoView)
    }

    func didChangeCallState(_ state: String) {
        statusLabel.text = "Call: \(state.uppercased())"

        switch state {
        case "active", "answered":
            callButton.isEnabled = false
            hangupButton.isEnabled = true
        case "ended", "failed":
            callButton.isEnabled = true
            hangupButton.isEnabled = false
        default:
            break
        }
    }

    func didReceiveMessage(from: String, body: String) {
        showAlert(title: "Message from \(from)", message: body)
    }

    func didReceiveIncomingCall(from: String, sessionId: String) {
        showIncomingCallAlert(from: from, sessionId: sessionId)
    }

    func didChangeRegistrationStatus(_ registered: Bool) {
        if registered {
            statusLabel.text = "Registered"
            registrationStack.isHidden = true
            callStack.isHidden = false
        } else {
            statusLabel.text = "Not Registered"
            registrationStack.isHidden = false
            callStack.isHidden = true
        }
    }

    func didConnect() {
        statusLabel.text = "Connected"
        registrationStack.isHidden = false
        webRTCService.startSession(localVideoView: localVideoView)
    }

    func didDisconnect() {
        statusLabel.text = "Disconnected"
        connectButton.isEnabled = true
    }

    func didReceiveError(_ error: String) {
        showAlert(title: "Error", message: error)
    }
}
```

---

## SIP MESSAGE

### Send/Receive Messages

```swift
// ส่งข้อความ (ระหว่างโทร)
webRTCService.sendMessage(body: "Hello from iOS!")

// รับข้อความ (ใน delegate)
func didReceiveMessage(from: String, body: String) {
    showAlert(title: "Message from \(from)", message: body)
}
```

---

## Troubleshooting

1. **Camera not working**: ตรวจสอบ Info.plist permissions และ AVCaptureDevice.requestAccess
2. **No audio**: ตรวจสอบ AVAudioSession category
3. **Build errors**: ตรวจสอบ Bitcode disabled สำหรับ WebRTC
4. **App rejected**: ตรวจสอบ background modes ถ้าใช้ VoIP

### Audio Session Setup

```swift
// Add in AppDelegate or ViewController
func configureAudioSession() {
    let audioSession = AVAudioSession.sharedInstance()
    do {
        try audioSession.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker])
        try audioSession.setActive(true)
    } catch {
        print("Audio session error: \(error)")
    }
}
```

---

## Additional Resources

- [WebRTC iOS SDK](https://cocoapods.org/pods/WebRTC-SDK)
- [Starscream WebSocket](https://github.com/daltoniam/Starscream)
- [K2 Gateway WebRTC Guide](../WebRTC.md)
- [WebRTC for iOS](https://webrtc.github.io/webrtc-org/native-code/ios/)
