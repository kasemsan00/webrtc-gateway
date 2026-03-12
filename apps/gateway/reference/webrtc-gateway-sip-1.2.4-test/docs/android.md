# Android WebRTC Integration Guide

คู่มือการใช้งาน K2 Gateway กับ Android Native สำหรับการโทร SIP/WebRTC

## Prerequisites

### Dependencies (build.gradle)

```gradle
dependencies {
    // WebRTC
    implementation 'io.getstream:stream-webrtc-android:1.1.1'

    // OkHttp for WebSocket
    implementation 'com.squareup.okhttp3:okhttp:4.12.0'

    // JSON parsing
    implementation 'com.google.code.gson:gson:2.10.1'

    // Coroutines
    implementation 'org.jetbrains.kotlinx:kotlinx-coroutines-android:1.7.3'
}
```

### Permissions (AndroidManifest.xml)

```xml
<uses-permission android:name="android.permission.INTERNET" />
<uses-permission android:name="android.permission.CAMERA" />
<uses-permission android:name="android.permission.RECORD_AUDIO" />
<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS" />
<uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />

<uses-feature android:name="android.hardware.camera" android:required="true" />
<uses-feature android:name="android.hardware.camera.autofocus" android:required="false" />
```

---

## Basic Implementation

### 1. WebRTC Service

```kotlin
// K2WebRTCService.kt
package com.example.k2gateway

import android.content.Context
import android.util.Log
import com.google.gson.Gson
import com.google.gson.JsonObject
import kotlinx.coroutines.*
import okhttp3.*
import org.webrtc.*
import java.util.concurrent.TimeUnit

class K2WebRTCService(private val context: Context) {

    companion object {
        private const val TAG = "K2WebRTC"
        private val ICE_SERVERS = listOf(
            PeerConnection.IceServer.builder("turn:turn.ttrs.or.th:3478?transport=udp")
                .setUsername("turn01")
                .setPassword("Test1234")
                .createIceServer()
        )
    }

    // WebRTC components
    private var peerConnectionFactory: PeerConnectionFactory? = null
    private var peerConnection: PeerConnection? = null
    private var localVideoTrack: VideoTrack? = null
    private var localAudioTrack: AudioTrack? = null
    private var videoCapturer: CameraVideoCapturer? = null
    private var surfaceTextureHelper: SurfaceTextureHelper? = null

    // WebSocket
    private var webSocket: WebSocket? = null
    private val client = OkHttpClient.Builder()
        .readTimeout(30, TimeUnit.SECONDS)
        .build()
    private val gson = Gson()

    // State
    private var sessionId: String? = null
    private var eglBase: EglBase? = null

    // Callbacks
    var onLocalStream: ((VideoTrack?, AudioTrack?) -> Unit)? = null
    var onRemoteStream: ((VideoTrack?, AudioTrack?) -> Unit)? = null
    var onCallState: ((String) -> Unit)? = null
    var onMessage: ((String, String) -> Unit)? = null
    var onIncomingCall: ((String, String) -> Unit)? = null
    var onRegistered: ((Boolean) -> Unit)? = null
    var onConnected: (() -> Unit)? = null
    var onDisconnected: (() -> Unit)? = null
    var onError: ((String) -> Unit)? = null

    // Initialize WebRTC
    fun initialize() {
        eglBase = EglBase.create()

        val options = PeerConnectionFactory.InitializationOptions.builder(context)
            .setEnableInternalTracer(true)
            .createInitializationOptions()
        PeerConnectionFactory.initialize(options)

        val encoderFactory = DefaultVideoEncoderFactory(
            eglBase?.eglBaseContext, true, true
        )
        val decoderFactory = DefaultVideoDecoderFactory(eglBase?.eglBaseContext)

        peerConnectionFactory = PeerConnectionFactory.builder()
            .setVideoEncoderFactory(encoderFactory)
            .setVideoDecoderFactory(decoderFactory)
            .createPeerConnectionFactory()
    }

    // Connect to K2 Gateway
    fun connect(wsUrl: String) {
        val request = Request.Builder()
            .url(wsUrl)
            .build()

        webSocket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.d(TAG, "WebSocket connected")
                onConnected?.invoke()
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                handleMessage(text)
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WebSocket closing: $reason")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WebSocket closed: $reason")
                onDisconnected?.invoke()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.e(TAG, "WebSocket error: ${t.message}")
                onError?.invoke(t.message ?: "Unknown error")
            }
        })
    }

    // Handle incoming messages
    private fun handleMessage(text: String) {
        try {
            val json = gson.fromJson(text, JsonObject::class.java)
            val type = json.get("type")?.asString ?: return

            when (type) {
                "answer" -> handleAnswer(json)
                "state" -> {
                    json.get("sessionId")?.asString?.let { sessionId = it }
                    onCallState?.invoke(json.get("state")?.asString ?: "unknown")
                }
                "incoming" -> {
                    onIncomingCall?.invoke(
                        json.get("from")?.asString ?: "Unknown",
                        json.get("sessionId")?.asString ?: ""
                    )
                }
                "registerStatus" -> {
                    onRegistered?.invoke(json.get("registered")?.asBoolean ?: false)
                }
                "message" -> {
                    onMessage?.invoke(
                        json.get("from")?.asString ?: "",
                        json.get("body")?.asString ?: ""
                    )
                }
                "error" -> {
                    onError?.invoke(json.get("error")?.asString ?: "Unknown error")
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error parsing message: ${e.message}")
        }
    }

    // Start media session
    fun startSession(localVideoView: SurfaceViewRenderer?) {
        CoroutineScope(Dispatchers.Main).launch {
            try {
                // Create local media
                createLocalMedia(localVideoView)

                // Create peer connection
                createPeerConnection()

                // Create and send offer
                createOffer()
            } catch (e: Exception) {
                Log.e(TAG, "Error starting session: ${e.message}")
                onError?.invoke(e.message ?: "Failed to start session")
            }
        }
    }

    private fun createLocalMedia(localVideoView: SurfaceViewRenderer?) {
        val factory = peerConnectionFactory ?: return

        // Audio
        val audioConstraints = MediaConstraints()
        val audioSource = factory.createAudioSource(audioConstraints)
        localAudioTrack = factory.createAudioTrack("audio0", audioSource)

        // Video
        videoCapturer = createCameraCapturer()
        videoCapturer?.let { capturer ->
            surfaceTextureHelper = SurfaceTextureHelper.create("CaptureThread", eglBase?.eglBaseContext)
            val videoSource = factory.createVideoSource(capturer.isScreencast)
            capturer.initialize(surfaceTextureHelper, context, videoSource.capturerObserver)
            capturer.startCapture(640, 480, 30)

            localVideoTrack = factory.createVideoTrack("video0", videoSource)
            localVideoView?.let { view ->
                view.init(eglBase?.eglBaseContext, null)
                view.setMirror(true)
                localVideoTrack?.addSink(view)
            }
        }

        onLocalStream?.invoke(localVideoTrack, localAudioTrack)
    }

    private fun createCameraCapturer(): CameraVideoCapturer? {
        val enumerator = Camera2Enumerator(context)

        // Try front camera first
        for (deviceName in enumerator.deviceNames) {
            if (enumerator.isFrontFacing(deviceName)) {
                return enumerator.createCapturer(deviceName, null)
            }
        }

        // Fallback to any camera
        for (deviceName in enumerator.deviceNames) {
            return enumerator.createCapturer(deviceName, null)
        }

        return null
    }

    private fun createPeerConnection() {
        val factory = peerConnectionFactory ?: return

        val rtcConfig = PeerConnection.RTCConfiguration(ICE_SERVERS).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
        }

        peerConnection = factory.createPeerConnection(rtcConfig, object : PeerConnection.Observer {
            override fun onIceCandidate(candidate: IceCandidate?) {
                Log.d(TAG, "ICE candidate: ${candidate?.sdp}")
            }

            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {
                Log.d(TAG, "ICE connection state: $state")
            }

            override fun onTrack(transceiver: RtpTransceiver?) {
                transceiver?.receiver?.track()?.let { track ->
                    Log.d(TAG, "Remote track received: ${track.kind()}")
                    when (track) {
                        is VideoTrack -> onRemoteStream?.invoke(track, null)
                        is AudioTrack -> onRemoteStream?.invoke(null, track)
                    }
                }
            }

            override fun onSignalingChange(state: PeerConnection.SignalingState?) {}
            override fun onIceConnectionReceivingChange(receiving: Boolean) {}
            override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {}
            override fun onIceCandidatesRemoved(candidates: Array<out IceCandidate>?) {}
            override fun onAddStream(stream: MediaStream?) {}
            override fun onRemoveStream(stream: MediaStream?) {}
            override fun onDataChannel(channel: DataChannel?) {}
            override fun onRenegotiationNeeded() {}
        })

        // Add local tracks
        localAudioTrack?.let {
            peerConnection?.addTrack(it, listOf("stream0"))
        }
        localVideoTrack?.let {
            peerConnection?.addTrack(it, listOf("stream0"))
        }
    }

    private fun createOffer() {
        val constraints = MediaConstraints().apply {
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveAudio", "true"))
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveVideo", "true"))
        }

        peerConnection?.createOffer(object : SdpObserver {
            override fun onCreateSuccess(sdp: SessionDescription?) {
                sdp?.let {
                    peerConnection?.setLocalDescription(object : SdpObserver {
                        override fun onSetSuccess() {
                            // Wait for ICE gathering then send offer
                            CoroutineScope(Dispatchers.Main).launch {
                                delay(2000) // Wait for ICE candidates
                                sendOffer(peerConnection?.localDescription?.description ?: "")
                            }
                        }
                        override fun onSetFailure(error: String?) {
                            Log.e(TAG, "Set local description failed: $error")
                        }
                        override fun onCreateSuccess(sdp: SessionDescription?) {}
                        override fun onCreateFailure(error: String?) {}
                    }, it)
                }
            }

            override fun onCreateFailure(error: String?) {
                Log.e(TAG, "Create offer failed: $error")
            }

            override fun onSetSuccess() {}
            override fun onSetFailure(error: String?) {}
        }, constraints)
    }

    private fun sendOffer(sdp: String) {
        val message = JsonObject().apply {
            addProperty("type", "offer")
            addProperty("sdp", sdp)
        }
        send(message.toString())
    }

    private fun handleAnswer(json: JsonObject) {
        val sdp = json.get("sdp")?.asString ?: return
        sessionId = json.get("sessionId")?.asString

        val answer = SessionDescription(SessionDescription.Type.ANSWER, sdp)
        peerConnection?.setRemoteDescription(object : SdpObserver {
            override fun onSetSuccess() {
                Log.d(TAG, "Remote description set successfully")
            }
            override fun onSetFailure(error: String?) {
                Log.e(TAG, "Set remote description failed: $error")
            }
            override fun onCreateSuccess(sdp: SessionDescription?) {}
            override fun onCreateFailure(error: String?) {}
        }, answer)
    }

    // SIP Registration
    fun register(domain: String, username: String, password: String, port: Int = 5060) {
        val message = JsonObject().apply {
            addProperty("type", "register")
            addProperty("sipDomain", domain)
            addProperty("sipUsername", username)
            addProperty("sipPassword", password)
            addProperty("sipPort", port)
        }
        send(message.toString())
    }

    fun unregister() {
        send("""{"type": "unregister"}""")
    }

    // Call controls
    fun call(destination: String) {
        sessionId?.let { sid ->
            val message = JsonObject().apply {
                addProperty("type", "call")
                addProperty("sessionId", sid)
                addProperty("destination", destination)
            }
            send(message.toString())
        }
    }

    fun hangup() {
        sessionId?.let { sid ->
            val message = JsonObject().apply {
                addProperty("type", "hangup")
                addProperty("sessionId", sid)
            }
            send(message.toString())
        }
    }

    fun acceptCall(incomingSessionId: String) {
        val message = JsonObject().apply {
            addProperty("type", "accept")
            addProperty("sessionId", incomingSessionId)
        }
        send(message.toString())
    }

    fun rejectCall(incomingSessionId: String) {
        val message = JsonObject().apply {
            addProperty("type", "reject")
            addProperty("sessionId", incomingSessionId)
        }
        send(message.toString())
    }

    // DTMF
    fun sendDTMF(digits: String) {
        sessionId?.let { sid ->
            val message = JsonObject().apply {
                addProperty("type", "dtmf")
                addProperty("sessionId", sid)
                addProperty("digits", digits)
            }
            send(message.toString())
        }
    }

    // SIP MESSAGE
    fun sendMessage(body: String, contentType: String = "text/plain;charset=UTF-8") {
        val message = JsonObject().apply {
            addProperty("type", "send_message")
            addProperty("body", body)
            addProperty("contentType", contentType)
        }
        send(message.toString())
    }

    // Mute controls
    fun toggleAudioMute(): Boolean {
        localAudioTrack?.let {
            it.setEnabled(!it.enabled())
            return !it.enabled()
        }
        return false
    }

    fun toggleVideoMute(): Boolean {
        localVideoTrack?.let {
            it.setEnabled(!it.enabled())
            return !it.enabled()
        }
        return false
    }

    // Switch camera
    fun switchCamera() {
        videoCapturer?.switchCamera(null)
    }

    // Send WebSocket message
    private fun send(message: String) {
        webSocket?.send(message)
    }

    // Cleanup
    fun cleanup() {
        videoCapturer?.stopCapture()
        videoCapturer?.dispose()
        videoCapturer = null

        localVideoTrack?.dispose()
        localVideoTrack = null

        localAudioTrack?.dispose()
        localAudioTrack = null

        peerConnection?.close()
        peerConnection = null

        surfaceTextureHelper?.dispose()
        surfaceTextureHelper = null

        sessionId = null
    }

    fun disconnect() {
        cleanup()
        webSocket?.close(1000, "User disconnected")
        webSocket = null

        peerConnectionFactory?.dispose()
        peerConnectionFactory = null

        eglBase?.release()
        eglBase = null
    }

    fun getEglBase(): EglBase? = eglBase
}
```

---

## Reconnection Support

### K2WebRTCService with Auto Reconnect

```kotlin
// K2WebRTCServiceWithReconnect.kt
package com.example.k2gateway

import android.content.Context
import android.util.Log
import com.google.gson.Gson
import com.google.gson.JsonObject
import kotlinx.coroutines.*
import okhttp3.*
import org.webrtc.*
import java.util.concurrent.TimeUnit
import kotlin.math.min
import kotlin.math.pow
import kotlin.random.Random

// Reconnection configuration
data class ReconnectConfig(
    val maxAttempts: Int = 5,           // Maximum retry attempts
    val baseDelayMs: Long = 1000,       // Initial delay in milliseconds
    val maxDelayMs: Long = 30000,       // Maximum delay in milliseconds
    val backoffMultiplier: Double = 2.0 // Exponential backoff multiplier
)

enum class ConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    RECONNECTING
}

class K2WebRTCService(private val context: Context) {

    companion object {
        private const val TAG = "K2WebRTC"
        private val ICE_SERVERS = listOf(
            PeerConnection.IceServer.builder("turn:turn.ttrs.or.th:3478?transport=udp")
                .setUsername("turn01")
                .setPassword("Test1234")
                .createIceServer()
        )
    }

    // WebRTC components
    private var peerConnectionFactory: PeerConnectionFactory? = null
    private var peerConnection: PeerConnection? = null
    private var localVideoTrack: VideoTrack? = null
    private var localAudioTrack: AudioTrack? = null
    private var videoCapturer: CameraVideoCapturer? = null
    private var surfaceTextureHelper: SurfaceTextureHelper? = null

    // WebSocket
    private var webSocket: WebSocket? = null
    private val client = OkHttpClient.Builder()
        .readTimeout(30, TimeUnit.SECONDS)
        .build()
    private val gson = Gson()

    // State
    private var sessionId: String? = null
    private var eglBase: EglBase? = null

    // Reconnection state
    private var wsUrl: String = ""
    private var reconnectAttempts: Int = 0
    private var reconnectJob: Job? = null
    private var isManualDisconnect: Boolean = false
    private var connectionState: ConnectionState = ConnectionState.DISCONNECTED
        private set(value) {
            field = value
            onConnectionStateChange?.invoke(value)
        }

    var reconnectConfig = ReconnectConfig()

    // SIP credentials for re-registration
    private var sipCredentials: SipCredentials? = null

    private data class SipCredentials(
        val domain: String,
        val username: String,
        val password: String,
        val port: Int
    )

    // Callbacks
    var onLocalStream: ((VideoTrack?, AudioTrack?) -> Unit)? = null
    var onRemoteStream: ((VideoTrack?, AudioTrack?) -> Unit)? = null
    var onCallState: ((String) -> Unit)? = null
    var onMessage: ((String, String) -> Unit)? = null
    var onIncomingCall: ((String, String) -> Unit)? = null
    var onRegistered: ((Boolean) -> Unit)? = null
    var onConnected: (() -> Unit)? = null
    var onDisconnected: (() -> Unit)? = null
    var onError: ((String) -> Unit)? = null

    // New callbacks for connection state
    var onConnectionStateChange: ((ConnectionState) -> Unit)? = null
    var onReconnecting: ((attempt: Int, maxAttempts: Int) -> Unit)? = null
    var onReconnectFailed: (() -> Unit)? = null

    // Initialize WebRTC
    fun initialize() {
        eglBase = EglBase.create()

        val options = PeerConnectionFactory.InitializationOptions.builder(context)
            .setEnableInternalTracer(true)
            .createInitializationOptions()
        PeerConnectionFactory.initialize(options)

        val encoderFactory = DefaultVideoEncoderFactory(
            eglBase?.eglBaseContext, true, true
        )
        val decoderFactory = DefaultVideoDecoderFactory(eglBase?.eglBaseContext)

        peerConnectionFactory = PeerConnectionFactory.builder()
            .setVideoEncoderFactory(encoderFactory)
            .setVideoDecoderFactory(decoderFactory)
            .createPeerConnectionFactory()
    }

    // Calculate delay with exponential backoff
    private fun getReconnectDelay(): Long {
        val delay = min(
            reconnectConfig.baseDelayMs * reconnectConfig.backoffMultiplier.pow(reconnectAttempts.toDouble()),
            reconnectConfig.maxDelayMs.toDouble()
        ).toLong()
        // Add jitter (±20%) to prevent thundering herd
        val jitter = (delay * 0.2 * (Random.nextDouble() * 2 - 1)).toLong()
        return delay + jitter
    }

    // Schedule reconnection attempt
    private fun scheduleReconnect() {
        if (reconnectAttempts >= reconnectConfig.maxAttempts) {
            Log.d(TAG, "Max reconnect attempts reached")
            onReconnectFailed?.invoke()
            return
        }

        val delay = getReconnectDelay()
        reconnectAttempts++

        Log.d(TAG, "Reconnecting in ${delay}ms (attempt $reconnectAttempts/${reconnectConfig.maxAttempts})")
        onReconnecting?.invoke(reconnectAttempts, reconnectConfig.maxAttempts)

        reconnectJob = CoroutineScope(Dispatchers.Main).launch {
            delay(delay)
            establishConnection()
        }
    }

    // Cancel pending reconnection
    fun cancelReconnect() {
        reconnectJob?.cancel()
        reconnectJob = null
        reconnectAttempts = 0
    }

    // Manual reconnect
    fun reconnect() {
        cancelReconnect()
        cleanup()
        reconnectAttempts = 0
        isManualDisconnect = false
        establishConnection()
    }

    // Connect to K2 Gateway
    fun connect(wsUrl: String) {
        this.wsUrl = wsUrl
        isManualDisconnect = false
        reconnectAttempts = 0
        establishConnection()
    }

    private fun establishConnection() {
        connectionState = if (reconnectAttempts > 0) ConnectionState.RECONNECTING else ConnectionState.CONNECTING

        val request = Request.Builder()
            .url(wsUrl)
            .build()

        webSocket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.d(TAG, "WebSocket connected")
                reconnectAttempts = 0
                connectionState = ConnectionState.CONNECTED
                onConnected?.invoke()

                // Re-register if we have stored credentials
                sipCredentials?.let { creds ->
                    Log.d(TAG, "Re-registering after reconnect...")
                    register(creds.domain, creds.username, creds.password, creds.port)
                }
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                handleMessage(text)
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WebSocket closing: $reason")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WebSocket closed: $reason")
                connectionState = ConnectionState.DISCONNECTED
                onDisconnected?.invoke()

                if (!isManualDisconnect) {
                    scheduleReconnect()
                }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.e(TAG, "WebSocket error: ${t.message}")
                connectionState = ConnectionState.DISCONNECTED
                onError?.invoke(t.message ?: "Unknown error")

                if (!isManualDisconnect) {
                    scheduleReconnect()
                }
            }
        })
    }

    // Handle incoming messages
    private fun handleMessage(text: String) {
        try {
            val json = gson.fromJson(text, JsonObject::class.java)
            val type = json.get("type")?.asString ?: return

            when (type) {
                "answer" -> handleAnswer(json)
                "state" -> {
                    json.get("sessionId")?.asString?.let { sessionId = it }
                    onCallState?.invoke(json.get("state")?.asString ?: "unknown")
                }
                "incoming" -> {
                    onIncomingCall?.invoke(
                        json.get("from")?.asString ?: "Unknown",
                        json.get("sessionId")?.asString ?: ""
                    )
                }
                "registerStatus" -> {
                    onRegistered?.invoke(json.get("registered")?.asBoolean ?: false)
                }
                "message" -> {
                    onMessage?.invoke(
                        json.get("from")?.asString ?: "",
                        json.get("body")?.asString ?: ""
                    )
                }
                "error" -> {
                    onError?.invoke(json.get("error")?.asString ?: "Unknown error")
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error parsing message: ${e.message}")
        }
    }

    // Start media session
    fun startSession(localVideoView: SurfaceViewRenderer?) {
        CoroutineScope(Dispatchers.Main).launch {
            try {
                createLocalMedia(localVideoView)
                createPeerConnection()
                createOffer()
            } catch (e: Exception) {
                Log.e(TAG, "Error starting session: ${e.message}")
                onError?.invoke(e.message ?: "Failed to start session")
            }
        }
    }

    private fun createLocalMedia(localVideoView: SurfaceViewRenderer?) {
        val factory = peerConnectionFactory ?: return

        // Audio
        val audioConstraints = MediaConstraints()
        val audioSource = factory.createAudioSource(audioConstraints)
        localAudioTrack = factory.createAudioTrack("audio0", audioSource)

        // Video
        videoCapturer = createCameraCapturer()
        videoCapturer?.let { capturer ->
            surfaceTextureHelper = SurfaceTextureHelper.create("CaptureThread", eglBase?.eglBaseContext)
            val videoSource = factory.createVideoSource(capturer.isScreencast)
            capturer.initialize(surfaceTextureHelper, context, videoSource.capturerObserver)
            capturer.startCapture(640, 480, 30)

            localVideoTrack = factory.createVideoTrack("video0", videoSource)
            localVideoView?.let { view ->
                view.init(eglBase?.eglBaseContext, null)
                view.setMirror(true)
                localVideoTrack?.addSink(view)
            }
        }

        onLocalStream?.invoke(localVideoTrack, localAudioTrack)
    }

    private fun createCameraCapturer(): CameraVideoCapturer? {
        val enumerator = Camera2Enumerator(context)

        for (deviceName in enumerator.deviceNames) {
            if (enumerator.isFrontFacing(deviceName)) {
                return enumerator.createCapturer(deviceName, null)
            }
        }

        for (deviceName in enumerator.deviceNames) {
            return enumerator.createCapturer(deviceName, null)
        }

        return null
    }

    private fun createPeerConnection() {
        val factory = peerConnectionFactory ?: return

        val rtcConfig = PeerConnection.RTCConfiguration(ICE_SERVERS).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
        }

        peerConnection = factory.createPeerConnection(rtcConfig, object : PeerConnection.Observer {
            override fun onIceCandidate(candidate: IceCandidate?) {}
            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {
                Log.d(TAG, "ICE connection state: $state")
            }
            override fun onTrack(transceiver: RtpTransceiver?) {
                transceiver?.receiver?.track()?.let { track ->
                    when (track) {
                        is VideoTrack -> onRemoteStream?.invoke(track, null)
                        is AudioTrack -> onRemoteStream?.invoke(null, track)
                    }
                }
            }
            override fun onSignalingChange(state: PeerConnection.SignalingState?) {}
            override fun onIceConnectionReceivingChange(receiving: Boolean) {}
            override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {}
            override fun onIceCandidatesRemoved(candidates: Array<out IceCandidate>?) {}
            override fun onAddStream(stream: MediaStream?) {}
            override fun onRemoveStream(stream: MediaStream?) {}
            override fun onDataChannel(channel: DataChannel?) {}
            override fun onRenegotiationNeeded() {}
        })

        localAudioTrack?.let { peerConnection?.addTrack(it, listOf("stream0")) }
        localVideoTrack?.let { peerConnection?.addTrack(it, listOf("stream0")) }
    }

    private fun createOffer() {
        val constraints = MediaConstraints().apply {
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveAudio", "true"))
            mandatory.add(MediaConstraints.KeyValuePair("OfferToReceiveVideo", "true"))
        }

        peerConnection?.createOffer(object : SdpObserver {
            override fun onCreateSuccess(sdp: SessionDescription?) {
                sdp?.let {
                    peerConnection?.setLocalDescription(object : SdpObserver {
                        override fun onSetSuccess() {
                            CoroutineScope(Dispatchers.Main).launch {
                                delay(2000)
                                sendOffer(peerConnection?.localDescription?.description ?: "")
                            }
                        }
                        override fun onSetFailure(error: String?) {
                            Log.e(TAG, "Set local description failed: $error")
                        }
                        override fun onCreateSuccess(sdp: SessionDescription?) {}
                        override fun onCreateFailure(error: String?) {}
                    }, it)
                }
            }
            override fun onCreateFailure(error: String?) {
                Log.e(TAG, "Create offer failed: $error")
            }
            override fun onSetSuccess() {}
            override fun onSetFailure(error: String?) {}
        }, constraints)
    }

    private fun sendOffer(sdp: String) {
        val message = JsonObject().apply {
            addProperty("type", "offer")
            addProperty("sdp", sdp)
        }
        send(message.toString())
    }

    private fun handleAnswer(json: JsonObject) {
        val sdp = json.get("sdp")?.asString ?: return
        sessionId = json.get("sessionId")?.asString

        val answer = SessionDescription(SessionDescription.Type.ANSWER, sdp)
        peerConnection?.setRemoteDescription(object : SdpObserver {
            override fun onSetSuccess() {
                Log.d(TAG, "Remote description set successfully")
            }
            override fun onSetFailure(error: String?) {
                Log.e(TAG, "Set remote description failed: $error")
            }
            override fun onCreateSuccess(sdp: SessionDescription?) {}
            override fun onCreateFailure(error: String?) {}
        }, answer)
    }

    // SIP Registration (stores credentials)
    fun register(domain: String, username: String, password: String, port: Int = 5060) {
        // Store credentials for reconnection
        sipCredentials = SipCredentials(domain, username, password, port)

        val message = JsonObject().apply {
            addProperty("type", "register")
            addProperty("sipDomain", domain)
            addProperty("sipUsername", username)
            addProperty("sipPassword", password)
            addProperty("sipPort", port)
        }
        send(message.toString())
    }

    fun unregister() {
        sipCredentials = null
        send("""{"type": "unregister"}""")
    }

    // Call controls
    fun call(destination: String) {
        sessionId?.let { sid ->
            val message = JsonObject().apply {
                addProperty("type", "call")
                addProperty("sessionId", sid)
                addProperty("destination", destination)
            }
            send(message.toString())
        }
    }

    fun hangup() {
        sessionId?.let { sid ->
            val message = JsonObject().apply {
                addProperty("type", "hangup")
                addProperty("sessionId", sid)
            }
            send(message.toString())
        }
    }

    fun acceptCall(incomingSessionId: String) {
        val message = JsonObject().apply {
            addProperty("type", "accept")
            addProperty("sessionId", incomingSessionId)
        }
        send(message.toString())
    }

    fun rejectCall(incomingSessionId: String) {
        val message = JsonObject().apply {
            addProperty("type", "reject")
            addProperty("sessionId", incomingSessionId)
        }
        send(message.toString())
    }

    // DTMF
    fun sendDTMF(digits: String) {
        sessionId?.let { sid ->
            val message = JsonObject().apply {
                addProperty("type", "dtmf")
                addProperty("sessionId", sid)
                addProperty("digits", digits)
            }
            send(message.toString())
        }
    }

    // SIP MESSAGE
    fun sendMessage(body: String, contentType: String = "text/plain;charset=UTF-8") {
        val message = JsonObject().apply {
            addProperty("type", "send_message")
            addProperty("body", body)
            addProperty("contentType", contentType)
        }
        send(message.toString())
    }

    // Mute controls
    fun toggleAudioMute(): Boolean {
        localAudioTrack?.let {
            it.setEnabled(!it.enabled())
            return !it.enabled()
        }
        return false
    }

    fun toggleVideoMute(): Boolean {
        localVideoTrack?.let {
            it.setEnabled(!it.enabled())
            return !it.enabled()
        }
        return false
    }

    // Switch camera
    fun switchCamera() {
        videoCapturer?.switchCamera(null)
    }

    // Get current connection state
    fun getConnectionState(): ConnectionState = connectionState

    // Send WebSocket message
    private fun send(message: String) {
        if (webSocket != null) {
            webSocket?.send(message)
        } else {
            Log.w(TAG, "WebSocket not connected, message not sent")
        }
    }

    // Cleanup
    fun cleanup() {
        videoCapturer?.stopCapture()
        videoCapturer?.dispose()
        videoCapturer = null

        localVideoTrack?.dispose()
        localVideoTrack = null

        localAudioTrack?.dispose()
        localAudioTrack = null

        peerConnection?.close()
        peerConnection = null

        surfaceTextureHelper?.dispose()
        surfaceTextureHelper = null

        sessionId = null
    }

    fun disconnect() {
        isManualDisconnect = true
        cancelReconnect()
        cleanup()
        sipCredentials = null
        webSocket?.close(1000, "User disconnected")
        webSocket = null

        peerConnectionFactory?.dispose()
        peerConnectionFactory = null

        eglBase?.release()
        eglBase = null

        connectionState = ConnectionState.DISCONNECTED
    }

    fun getEglBase(): EglBase? = eglBase
}
```

### Using Reconnection in Activity

```kotlin
// VideoCallActivity+Reconnect.kt

class VideoCallActivity : AppCompatActivity() {

    private lateinit var webRTCService: K2WebRTCService
    private lateinit var tvStatus: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // ... setup views

        initWebRTCWithReconnection()
    }

    private fun initWebRTCWithReconnection() {
        webRTCService = K2WebRTCService(this)
        webRTCService.initialize()

        // Configure reconnection
        webRTCService.reconnectConfig = ReconnectConfig(
            maxAttempts = 5,
            baseDelayMs = 1000,
            maxDelayMs = 30000,
            backoffMultiplier = 2.0
        )

        // Connection state callback
        webRTCService.onConnectionStateChange = { state ->
            runOnUiThread {
                when (state) {
                    ConnectionState.DISCONNECTED -> {
                        tvStatus.text = "Disconnected"
                        tvStatus.setBackgroundColor(Color.RED)
                    }
                    ConnectionState.CONNECTING -> {
                        tvStatus.text = "Connecting..."
                        tvStatus.setBackgroundColor(Color.YELLOW)
                    }
                    ConnectionState.CONNECTED -> {
                        tvStatus.text = "Connected"
                        tvStatus.setBackgroundColor(Color.GREEN)
                    }
                    ConnectionState.RECONNECTING -> {
                        tvStatus.text = "Reconnecting..."
                        tvStatus.setBackgroundColor(Color.parseColor("#FF9800"))
                    }
                }
            }
        }

        // Reconnection progress callback
        webRTCService.onReconnecting = { attempt, maxAttempts ->
            runOnUiThread {
                tvStatus.text = "Reconnecting ($attempt/$maxAttempts)..."
            }
        }

        // Reconnection failed callback
        webRTCService.onReconnectFailed = {
            runOnUiThread {
                AlertDialog.Builder(this)
                    .setTitle("Connection Lost")
                    .setMessage("Unable to reconnect to server. Would you like to try again?")
                    .setPositiveButton("Retry") { _, _ ->
                        webRTCService.reconnect()
                    }
                    .setNegativeButton("Cancel", null)
                    .show()
            }
        }

        // ... other callbacks
    }
}
```

### Jetpack Compose with Reconnection

```kotlin
// VideoCallViewModel.kt
class VideoCallViewModel(application: Application) : AndroidViewModel(application) {

    private val webRTCService = K2WebRTCService(application)

    val connectionState = MutableStateFlow(ConnectionState.DISCONNECTED)
    val reconnectInfo = MutableStateFlow<String?>(null)
    val showReconnectDialog = MutableStateFlow(false)

    init {
        webRTCService.initialize()

        // Configure reconnection
        webRTCService.reconnectConfig = ReconnectConfig(
            maxAttempts = 5,
            baseDelayMs = 1000,
            maxDelayMs = 30000,
            backoffMultiplier = 2.0
        )

        webRTCService.onConnectionStateChange = { state ->
            connectionState.value = state
        }

        webRTCService.onReconnecting = { attempt, maxAttempts ->
            reconnectInfo.value = "Reconnecting ($attempt/$maxAttempts)..."
        }

        webRTCService.onReconnectFailed = {
            reconnectInfo.value = null
            showReconnectDialog.value = true
        }
    }

    fun connect(url: String) {
        webRTCService.connect(url)
    }

    fun reconnect() {
        showReconnectDialog.value = false
        webRTCService.reconnect()
    }

    fun dismissReconnectDialog() {
        showReconnectDialog.value = false
    }

    override fun onCleared() {
        super.onCleared()
        webRTCService.disconnect()
    }
}

// VideoCallScreen.kt
@Composable
fun VideoCallScreen(viewModel: VideoCallViewModel = viewModel()) {
    val connectionState by viewModel.connectionState.collectAsState()
    val reconnectInfo by viewModel.reconnectInfo.collectAsState()
    val showReconnectDialog by viewModel.showReconnectDialog.collectAsState()

    Box(modifier = Modifier.fillMaxSize()) {
        // Status indicator
        Text(
            text = when (connectionState) {
                ConnectionState.DISCONNECTED -> "Disconnected"
                ConnectionState.CONNECTING -> "Connecting..."
                ConnectionState.CONNECTED -> "Connected"
                ConnectionState.RECONNECTING -> reconnectInfo ?: "Reconnecting..."
            },
            color = Color.White,
            modifier = Modifier
                .align(Alignment.TopCenter)
                .background(
                    when (connectionState) {
                        ConnectionState.DISCONNECTED -> Color.Red
                        ConnectionState.CONNECTING -> Color.Yellow
                        ConnectionState.CONNECTED -> Color.Green
                        ConnectionState.RECONNECTING -> Color(0xFFFF9800)
                    }
                )
                .padding(8.dp)
        )

        // Reconnect dialog
        if (showReconnectDialog) {
            AlertDialog(
                onDismissRequest = { viewModel.dismissReconnectDialog() },
                title = { Text("Connection Lost") },
                text = { Text("Unable to reconnect to server. Would you like to try again?") },
                confirmButton = {
                    TextButton(onClick = { viewModel.reconnect() }) {
                        Text("Retry")
                    }
                },
                dismissButton = {
                    TextButton(onClick = { viewModel.dismissReconnectDialog() }) {
                        Text("Cancel")
                    }
                }
            )
        }
    }
}
```

---

## Activity Example

### 2. Video Call Activity

```kotlin
// VideoCallActivity.kt
package com.example.k2gateway

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import android.view.View
import android.widget.*
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import org.webrtc.SurfaceViewRenderer

class VideoCallActivity : AppCompatActivity() {

    companion object {
        private const val PERMISSION_REQUEST_CODE = 100
        private const val K2_GATEWAY_URL = "wss://k2-gateway.example.com/ws"
    }

    private lateinit var webRTCService: K2WebRTCService

    // Views
    private lateinit var localVideoView: SurfaceViewRenderer
    private lateinit var remoteVideoView: SurfaceViewRenderer
    private lateinit var btnConnect: Button
    private lateinit var btnRegister: Button
    private lateinit var btnCall: Button
    private lateinit var btnHangup: Button
    private lateinit var btnMuteAudio: Button
    private lateinit var btnMuteVideo: Button
    private lateinit var btnSwitchCamera: Button
    private lateinit var etDestination: EditText
    private lateinit var etSipDomain: EditText
    private lateinit var etSipUsername: EditText
    private lateinit var etSipPassword: EditText
    private lateinit var tvStatus: TextView
    private lateinit var layoutRegistration: LinearLayout
    private lateinit var layoutCall: LinearLayout

    private var isAudioMuted = false
    private var isVideoMuted = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_video_call)

        initViews()
        checkPermissions()
    }

    private fun initViews() {
        localVideoView = findViewById(R.id.localVideoView)
        remoteVideoView = findViewById(R.id.remoteVideoView)
        btnConnect = findViewById(R.id.btnConnect)
        btnRegister = findViewById(R.id.btnRegister)
        btnCall = findViewById(R.id.btnCall)
        btnHangup = findViewById(R.id.btnHangup)
        btnMuteAudio = findViewById(R.id.btnMuteAudio)
        btnMuteVideo = findViewById(R.id.btnMuteVideo)
        btnSwitchCamera = findViewById(R.id.btnSwitchCamera)
        etDestination = findViewById(R.id.etDestination)
        etSipDomain = findViewById(R.id.etSipDomain)
        etSipUsername = findViewById(R.id.etSipUsername)
        etSipPassword = findViewById(R.id.etSipPassword)
        tvStatus = findViewById(R.id.tvStatus)
        layoutRegistration = findViewById(R.id.layoutRegistration)
        layoutCall = findViewById(R.id.layoutCall)

        btnConnect.setOnClickListener { connect() }
        btnRegister.setOnClickListener { register() }
        btnCall.setOnClickListener { call() }
        btnHangup.setOnClickListener { hangup() }
        btnMuteAudio.setOnClickListener { toggleAudio() }
        btnMuteVideo.setOnClickListener { toggleVideo() }
        btnSwitchCamera.setOnClickListener { switchCamera() }

        // Initially hide call controls
        layoutCall.visibility = View.GONE
    }

    private fun checkPermissions() {
        val permissions = arrayOf(
            Manifest.permission.CAMERA,
            Manifest.permission.RECORD_AUDIO
        )

        val notGranted = permissions.filter {
            ContextCompat.checkSelfPermission(this, it) != PackageManager.PERMISSION_GRANTED
        }

        if (notGranted.isNotEmpty()) {
            ActivityCompat.requestPermissions(this, notGranted.toTypedArray(), PERMISSION_REQUEST_CODE)
        } else {
            initWebRTC()
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == PERMISSION_REQUEST_CODE) {
            if (grantResults.all { it == PackageManager.PERMISSION_GRANTED }) {
                initWebRTC()
            } else {
                Toast.makeText(this, "Permissions required", Toast.LENGTH_SHORT).show()
                finish()
            }
        }
    }

    private fun initWebRTC() {
        webRTCService = K2WebRTCService(this)
        webRTCService.initialize()

        // Initialize video views
        webRTCService.getEglBase()?.let { eglBase ->
            remoteVideoView.init(eglBase.eglBaseContext, null)
            remoteVideoView.setScalingType(org.webrtc.RendererCommon.ScalingType.SCALE_ASPECT_FIT)
        }

        // Setup callbacks
        webRTCService.onConnected = {
            runOnUiThread {
                tvStatus.text = "Connected"
                layoutRegistration.visibility = View.VISIBLE
            }
        }

        webRTCService.onDisconnected = {
            runOnUiThread {
                tvStatus.text = "Disconnected"
                btnConnect.isEnabled = true
            }
        }

        webRTCService.onRegistered = { registered ->
            runOnUiThread {
                if (registered) {
                    tvStatus.text = "Registered"
                    layoutRegistration.visibility = View.GONE
                    layoutCall.visibility = View.VISIBLE
                } else {
                    tvStatus.text = "Not Registered"
                    layoutRegistration.visibility = View.VISIBLE
                    layoutCall.visibility = View.GONE
                }
            }
        }

        webRTCService.onCallState = { state ->
            runOnUiThread {
                tvStatus.text = "Call: ${state.uppercase()}"
                when (state) {
                    "active", "answered" -> {
                        btnCall.isEnabled = false
                        btnHangup.isEnabled = true
                    }
                    "ended", "failed" -> {
                        btnCall.isEnabled = true
                        btnHangup.isEnabled = false
                    }
                }
            }
        }

        webRTCService.onRemoteStream = { videoTrack, _ ->
            runOnUiThread {
                videoTrack?.addSink(remoteVideoView)
            }
        }

        webRTCService.onIncomingCall = { from, sessionId ->
            runOnUiThread {
                showIncomingCallDialog(from, sessionId)
            }
        }

        webRTCService.onMessage = { from, body ->
            runOnUiThread {
                showMessageDialog(from, body)
            }
        }

        webRTCService.onError = { error ->
            runOnUiThread {
                Toast.makeText(this, "Error: $error", Toast.LENGTH_SHORT).show()
            }
        }
    }

    private fun connect() {
        btnConnect.isEnabled = false
        tvStatus.text = "Connecting..."
        webRTCService.connect(K2_GATEWAY_URL)
        webRTCService.startSession(localVideoView)
    }

    private fun register() {
        val domain = etSipDomain.text.toString()
        val username = etSipUsername.text.toString()
        val password = etSipPassword.text.toString()

        if (domain.isEmpty() || username.isEmpty() || password.isEmpty()) {
            Toast.makeText(this, "Please fill all fields", Toast.LENGTH_SHORT).show()
            return
        }

        webRTCService.register(domain, username, password)
    }

    private fun call() {
        val destination = etDestination.text.toString()
        if (destination.isEmpty()) {
            Toast.makeText(this, "Enter destination", Toast.LENGTH_SHORT).show()
            return
        }
        webRTCService.call(destination)
    }

    private fun hangup() {
        webRTCService.hangup()
    }

    private fun toggleAudio() {
        isAudioMuted = webRTCService.toggleAudioMute()
        btnMuteAudio.text = if (isAudioMuted) "Unmute" else "Mute"
    }

    private fun toggleVideo() {
        isVideoMuted = webRTCService.toggleVideoMute()
        btnMuteVideo.text = if (isVideoMuted) "Video On" else "Video Off"
    }

    private fun switchCamera() {
        webRTCService.switchCamera()
    }

    private fun showIncomingCallDialog(from: String, sessionId: String) {
        AlertDialog.Builder(this)
            .setTitle("Incoming Call")
            .setMessage("Call from: $from")
            .setPositiveButton("Accept") { _, _ ->
                webRTCService.acceptCall(sessionId)
            }
            .setNegativeButton("Reject") { _, _ ->
                webRTCService.rejectCall(sessionId)
            }
            .setCancelable(false)
            .show()
    }

    private fun showMessageDialog(from: String, body: String) {
        AlertDialog.Builder(this)
            .setTitle("Message from $from")
            .setMessage(body)
            .setPositiveButton("OK", null)
            .show()
    }

    override fun onDestroy() {
        super.onDestroy()
        webRTCService.disconnect()
    }
}
```

---

## Layout XML

### 3. activity_video_call.xml

```xml
<?xml version="1.0" encoding="utf-8"?>
<FrameLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="match_parent"
    android:background="#000000">

    <!-- Remote Video (Full Screen) -->
    <org.webrtc.SurfaceViewRenderer
        android:id="@+id/remoteVideoView"
        android:layout_width="match_parent"
        android:layout_height="match_parent" />

    <!-- Local Video (PIP) -->
    <org.webrtc.SurfaceViewRenderer
        android:id="@+id/localVideoView"
        android:layout_width="120dp"
        android:layout_height="160dp"
        android:layout_gravity="top|end"
        android:layout_marginTop="50dp"
        android:layout_marginEnd="16dp" />

    <!-- Status -->
    <TextView
        android:id="@+id/tvStatus"
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:layout_gravity="top|center_horizontal"
        android:layout_marginTop="16dp"
        android:text="Ready"
        android:textColor="#FFFFFF"
        android:textSize="16sp"
        android:background="#80000000"
        android:padding="8dp" />

    <!-- Connect Button -->
    <Button
        android:id="@+id/btnConnect"
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:layout_gravity="center"
        android:text="Connect" />

    <!-- Registration Form -->
    <LinearLayout
        android:id="@+id/layoutRegistration"
        android:layout_width="match_parent"
        android:layout_height="wrap_content"
        android:layout_gravity="bottom"
        android:orientation="vertical"
        android:padding="16dp"
        android:background="#CC000000"
        android:visibility="gone">

        <EditText
            android:id="@+id/etSipDomain"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:hint="SIP Domain"
            android:text="sipclient.ttrs.or.th"
            android:textColor="#FFFFFF"
            android:textColorHint="#888888"
            android:background="@android:drawable/editbox_background" />

        <EditText
            android:id="@+id/etSipUsername"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:layout_marginTop="8dp"
            android:hint="Username"
            android:textColor="#FFFFFF"
            android:textColorHint="#888888"
            android:background="@android:drawable/editbox_background" />

        <EditText
            android:id="@+id/etSipPassword"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:layout_marginTop="8dp"
            android:hint="Password"
            android:inputType="textPassword"
            android:textColor="#FFFFFF"
            android:textColorHint="#888888"
            android:background="@android:drawable/editbox_background" />

        <Button
            android:id="@+id/btnRegister"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:layout_marginTop="8dp"
            android:text="Register" />
    </LinearLayout>

    <!-- Call Controls -->
    <LinearLayout
        android:id="@+id/layoutCall"
        android:layout_width="match_parent"
        android:layout_height="wrap_content"
        android:layout_gravity="bottom"
        android:orientation="vertical"
        android:padding="16dp"
        android:background="#CC000000"
        android:visibility="gone">

        <EditText
            android:id="@+id/etDestination"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:hint="Destination (e.g. 9999)"
            android:text="9999"
            android:inputType="phone"
            android:textColor="#FFFFFF"
            android:textColorHint="#888888"
            android:background="@android:drawable/editbox_background" />

        <LinearLayout
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:layout_marginTop="8dp"
            android:orientation="horizontal">

            <Button
                android:id="@+id/btnCall"
                android:layout_width="0dp"
                android:layout_height="wrap_content"
                android:layout_weight="1"
                android:text="Call"
                android:backgroundTint="#4CAF50" />

            <Button
                android:id="@+id/btnHangup"
                android:layout_width="0dp"
                android:layout_height="wrap_content"
                android:layout_weight="1"
                android:layout_marginStart="8dp"
                android:text="Hangup"
                android:backgroundTint="#F44336"
                android:enabled="false" />
        </LinearLayout>

        <LinearLayout
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:layout_marginTop="8dp"
            android:orientation="horizontal">

            <Button
                android:id="@+id/btnMuteAudio"
                android:layout_width="0dp"
                android:layout_height="wrap_content"
                android:layout_weight="1"
                android:text="Mute" />

            <Button
                android:id="@+id/btnMuteVideo"
                android:layout_width="0dp"
                android:layout_height="wrap_content"
                android:layout_weight="1"
                android:layout_marginStart="4dp"
                android:text="Video Off" />

            <Button
                android:id="@+id/btnSwitchCamera"
                android:layout_width="0dp"
                android:layout_height="wrap_content"
                android:layout_weight="1"
                android:layout_marginStart="4dp"
                android:text="Flip" />
        </LinearLayout>
    </LinearLayout>
</FrameLayout>
```

---

## SIP MESSAGE

### Send/Receive Messages

```kotlin
// ส่งข้อความ (ระหว่างโทร)
webRTCService.sendMessage("Hello from Android!")

// รับข้อความ
webRTCService.onMessage = { from, body ->
    runOnUiThread {
        showMessageDialog(from, body)
    }
}
```

---

## ProGuard Rules

```proguard
# WebRTC
-keep class org.webrtc.** { *; }
-dontwarn org.webrtc.**

# OkHttp
-dontwarn okhttp3.**
-keep class okhttp3.** { *; }

# Gson
-keep class com.google.gson.** { *; }
```

---

## Troubleshooting

1. **Camera not working**: ตรวจสอบ runtime permissions
2. **No audio**: ตรวจสอบ RECORD_AUDIO permission
3. **WebSocket fails**: ตรวจสอบ INTERNET permission และ network security config
4. **Video freeze**: ตรวจสอบ TURN server connectivity

---

## Additional Resources

- [stream-webrtc-android](https://github.com/AgoraIO/stream-webrtc-android)
- [K2 Gateway WebRTC Guide](../WebRTC.md)
- [WebRTC for Android](https://webrtc.github.io/webrtc-org/native-code/android/)
