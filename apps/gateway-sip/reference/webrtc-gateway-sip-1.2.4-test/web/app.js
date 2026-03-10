// --- Configuration ---
const CONFIG = {
  wssUrl: `wss://${window.location.hostname}:8000/ws`, // Adjust port if needed or use relative
};

// Handle specific dev environment where port might differ or be behind proxy
// If hostname is "k2-gateway.kasemsan.com", use that.
if (window.location.hostname === "k2-gateway.kasemsan.com") {
  CONFIG.wssUrl = `wss://k2-gateway.kasemsan.com/ws`;
} else if (window.location.protocol === "file:") {
  CONFIG.wssUrl = "wss://k2-gateway.kasemsan.com/ws"; // Fallback for local file opening
}

// --- State ---
let state = {
  ws: null,
  pc: null,
  localStream: null,
  sessionId: null,
  callState: null, // Track call state: connecting, ringing, active, ended
  activeCallSessionId: null, // Session ID of active call (for hangup)
  mode: 'public', // 'public' or 'siptrunk'
  isMutedAudio: false,
  isMutedVideo: false,
  statsInterval: null,
  callTimerInterval: null,
  callStartTime: null,
  callCount: 0, // Track number of calls for debugging video issues
  pingInterval: null, // Auto-ping interval
  trunkId: null,
  trunkResolvePending: false,
  trunkResolvePayload: null,
  pendingRedirectUrl: null,
  pendingCallRequest: null,
  autoStartingSession: false,
  videoConfig: {
    maxBitrate: 1500, // kbps
    maxFramerate: 30, // fps
    useConstrainedBaseline: false,
    width: 640,
    height: 480,
  },
};

// --- UI Helpers ---
const $ = (id) => document.getElementById(id);

function log(msg, type = "info") {
  const div = document.createElement("div");
  div.className = `log-entry ${type}`;
  const time = new Date().toLocaleTimeString("en-US", { hour12: false });
  div.innerHTML = `<span class="time">${time}</span>${msg}`;
  $("logContainer").appendChild(div);
  $("logContainer").scrollTop = $("logContainer").scrollHeight;
  console.log(`[${type.toUpperCase()}]`, msg);
}

function clearLog() {
  $("logContainer").innerHTML = "";
}

function setStep(step, status) {
  // status: 'active', 'done', 'idle'
  const el = $(`step${step}`);
  el.className = `step-item ${status === "idle" ? "" : status}`;

  if (step === 1 && status === "done") {
    $("step1").querySelector(".step-icon").innerHTML = "✓";
    $("btnConnect").textContent = "Disconnect";
    $("btnConnect").classList.replace("btn-primary", "btn-danger");
  } else if (step === 1) {
    $("step1").querySelector(".step-icon").innerHTML = "1";
    $("btnConnect").textContent = "Connect";
    $("btnConnect").classList.replace("btn-danger", "btn-primary");
  }

  if (step === 2 && status === "done") {
    $("step2").querySelector(".step-icon").innerHTML = "✓";
    $("btnStartSession").textContent = "End Session";
    $("btnStartSession").classList.remove("btn-outline");
    $("btnStartSession").classList.add("btn-danger");
  } else if (step === 2 && status === "active") {
    // In progress
  } else if (step === 2) {
    $("step2").querySelector(".step-icon").innerHTML = "2";
    $("btnStartSession").textContent = "Start";
    $("btnStartSession").classList.add("btn-outline");
    $("btnStartSession").classList.remove("btn-danger");
  }
}

function setInput(val) {
  $("destination").value = val;
}

function toggleDialpad() {
  const dp = $("dialpad");
  dp.style.display = dp.style.display === "none" ? "grid" : "none";
}

// Switch between public and siptrunk modes
function switchMode(mode) {
  state.mode = mode;
  log(`Switched to ${mode.toUpperCase()} mode`, "info");
  
  // Update UI
  const publicSection = $("publicSection");
  const trunkSection = $("trunkSection");
  const radioPublic = $("modePublic");
  const radioTrunk = $("modeTrunk");
  
  if (mode === 'public') {
    if (publicSection) publicSection.style.display = "block";
    if (trunkSection) trunkSection.style.display = "none";
    if (radioPublic) radioPublic.checked = true;
  } else {
    if (publicSection) publicSection.style.display = "none";
    if (trunkSection) trunkSection.style.display = "block";
    if (radioTrunk) radioTrunk.checked = true;
  }
  
  updateCallButtonState();
}

// --- Logic ---

// 1. WebSocket
function toggleConnect() {
  if (state.ws) {
    state.ws.close();
  } else {
    connect();
  }
}

function connect(urlOverride) {
  const targetUrl = urlOverride || CONFIG.wssUrl;
  log(`Connecting to ${targetUrl}...`);
  $("wsStateText").textContent = "Connecting...";

  try {
    state.ws = new WebSocket(targetUrl);

    state.ws.onopen = () => {
      log("WebSocket Connected", "success");
      $("wsStateText").textContent = "Connected";
      $("connectionStatusDot").className = "status-dot connected";
      setStep(1, "done");
      setStep(2, "active"); // Ready for next step
      $("btnStartSession").disabled = false;
      updateTrunkUI(true); // Enable trunk resolve
      startPingInterval(); // Start auto-ping
      updateCallButtonState();

      if (state.trunkResolvePending && state.trunkResolvePayload) {
        const payload = state.trunkResolvePayload;
        const btnResolveTrunk = $("btnResolveTrunk");
        const trunkStatusBadge = $("trunkStatusBadge");
        
        if (btnResolveTrunk) btnResolveTrunk.disabled = true;
        if (trunkStatusBadge) {
          trunkStatusBadge.textContent = "Resolving...";
          trunkStatusBadge.style.background = "rgba(245, 158, 11, 0.3)";
          trunkStatusBadge.style.color = "#fbbf24";
        }
        state.ws.send(
          JSON.stringify({
            type: "trunk_resolve",
            sipDomain: payload.sipDomain,
            sipUsername: payload.sipUsername,
            sipPassword: payload.sipPassword,
            sipPort: payload.sipPort,
          }),
        );
      }
    };

    state.ws.onclose = () => {
      log("WebSocket Disconnected", "warning");
      $("wsStateText").textContent = "Disconnected";
      $("connectionStatusDot").className = "status-dot disconnected";
      cleanupSession();
      setStep(1, "idle");
      setStep(2, "idle");
      $("btnStartSession").disabled = true;
      updateTrunkUI(false); // Disable trunk resolve
      stopPingInterval(); // Stop auto-ping
      state.ws = null;
      updateCallButtonState();

      if (state.pendingRedirectUrl) {
        const redirectUrl = state.pendingRedirectUrl;
        state.pendingRedirectUrl = null;
        setTimeout(() => {
          log(`Redirecting to ${redirectUrl}`, "info");
          connect(redirectUrl);
        }, 200);
      }
    };

    state.ws.onerror = (e) => {
      log("WebSocket Error", "error");
      $("wsStateText").textContent = "Error";
    };

    state.ws.onmessage = handleMessage;
  } catch (e) {
    log("Connection failed: " + e.message, "error");
  }
}

function handleMessage(event) {
  const msg = JSON.parse(event.data);
  // log(`RX: ${msg.type}`, "info");

  switch (msg.type) {
    case "answer":
      handleAnswer(msg);
      break;
    case "pong":
      log("Pong received", "success");
      break;
    case "state":
      // Update sessionId if provided in state message (important for incoming calls)
      // BUT: don't set sessionId for "ended" state to avoid resurrecting old session
      if (msg.sessionId && msg.state !== "ended") {
        state.sessionId = msg.sessionId;
        $("sessionIdDisplay").textContent = state.sessionId;
      }
      handleCallState(msg.state);
      break;
    case "incoming":
      handleIncomingCall(msg);
      break;
    case "trunk_resolved":
      handleTrunkResolved(msg);
      break;
    case "trunk_redirect":
      handleTrunkRedirect(msg);
      break;
    case "trunk_not_found":
      handleTrunkNotFound(msg);
      break;
    case "trunk_not_ready":
      handleTrunkNotReady(msg);
      break;
    case "message":
      handleIncomingSIPMessage(msg);
      break;
    case "messageSent":
      log(`Message sent to ${msg.destination}`, "success");
      break;
    case "dtmf":
      handleReceivedDTMF(msg);
      break;
    case "error":
      log(`Server Error: ${msg.error}`, "error");
      // Handle "Session not found" error - treat as call ended
      if (msg.error && msg.error.includes("Session not found")) {
        log("Session not found on server, cleaning up call state", "warning");
        handleCallState("ended");
        state.activeCallSessionId = null;
      }
      break;
  }
}

function sendPing() {
  if (state.ws && state.ws.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify({ type: "ping" }));
    log("Ping sent", "info");
  } else {
    log("WebSocket not connected", "error");
  }
}

// Start auto-ping interval (every 20 seconds)
function startPingInterval() {
  // Clear any existing interval first
  stopPingInterval();

  log("Auto-ping started (every 20 seconds)", "info");
  state.pingInterval = setInterval(() => {
    if (state.ws && state.ws.readyState === WebSocket.OPEN) {
      state.ws.send(JSON.stringify({ type: "ping" }));
      // log("Auto-ping sent", "info"); // Uncomment for verbose logging
    }
  }, 20000); // 20 seconds
}

// Stop auto-ping interval
function stopPingInterval() {
  if (state.pingInterval) {
    clearInterval(state.pingInterval);
    state.pingInterval = null;
    log("Auto-ping stopped", "info");
  }
}

// 2. WebRTC Session
async function startSession() {
  // If already active, treat as End Session
  if (state.pc) {
    endSession();
    return;
  }

  try {
    log("Requesting Media Access...");
    $("rtcStateText").textContent = "Getting User Media...";

    state.localStream = await navigator.mediaDevices.getUserMedia({
      audio: true,
      video: {
        width: { ideal: state.videoConfig.width },
        height: { ideal: state.videoConfig.height },
        frameRate: { max: state.videoConfig.maxFramerate },
      },
    });

    $("localVideo").srcObject = state.localStream;
    log("Media Access Granted", "success");

    // Init PC
    log("Initializing PeerConnection...");
    state.pc = new RTCPeerConnection({
      iceServers: [
        {
          urls: "turn:turn.ttrs.or.th:3478?transport=udp",
          username: "turn01",
          credential: "Test1234",
        },
      ],
    });

    // ICE Events
    state.pc.oniceconnectionstatechange = () => {
      const s = state.pc.iceConnectionState;
      log(`ICE State: ${s}`, s === "connected" ? "success" : "info");
      $("iceStateDisplay").textContent = s;
      if (s === "disconnected" || s === "failed") {
        $("connectionStatusDot").className = "status-dot disconnected";
      }
    };

    state.pc.onsignalingstatechange = () => {
      $("sigStateDisplay").textContent = state.pc.signalingState;
    };

    // Tracks
    state.localStream.getTracks().forEach((track) => state.pc.addTrack(track, state.localStream));

    state.pc.ontrack = (event) => {
      log(`Remote Track: ${event.track.kind}`, "success");
      if (event.track.kind === "video") $("remoteVideo").srcObject = event.streams[0];
      if (event.track.kind === "audio") $("remoteAudio").srcObject = event.streams[0];
    };

    // Transceiver Codec preferences (Force H.264 only, let browser negotiate audio codec)
    // If useConstrainedBaseline is true, prefer profile-level-id starting with 42e0 (Constrained Baseline)
    const transceivers = state.pc.getTransceivers();
    transceivers.forEach((t) => {
      if (t.receiver.track.kind === "video") {
        const codecs = RTCRtpSender.getCapabilities("video").codecs;
        let h264Codecs = codecs.filter((c) => c.mimeType === "video/H264" && c.sdpFmtpLine && c.sdpFmtpLine.includes("packetization-mode=1"));

        // If constrained baseline is requested, filter for profile-level-id 42e0xx (Constrained Baseline)
        if (state.videoConfig.useConstrainedBaseline) {
          const constrainedBaseline = h264Codecs.filter((c) => c.sdpFmtpLine && c.sdpFmtpLine.includes("profile-level-id=42e0"));
          if (constrainedBaseline.length > 0) {
            h264Codecs = constrainedBaseline;
            log("Using H.264 Constrained Baseline Profile", "info");
          } else {
            log("H.264 Constrained Baseline not available, using default H.264", "warning");
          }
        }

        if (h264Codecs.length) t.setCodecPreferences(h264Codecs);
      }
      // Audio: No codec preference - let browser negotiate (Opus, PCMU, etc.)
    });

    // Offer
    const offer = await state.pc.createOffer();
    await state.pc.setLocalDescription(offer);

    // Wait for ICE Gathering (simple approach)
    if (state.pc.iceGatheringState !== "complete") {
      await new Promise((r) => {
        let timeout = setTimeout(r, 2000); // 2s timeout for candidates
        state.pc.onicegatheringstatechange = () => {
          if (state.pc.iceGatheringState === "complete") {
            clearTimeout(timeout);
            r();
          }
        };
      });
    }

    // Send
    state.ws.send(
      JSON.stringify({
        type: "offer",
        sdp: state.pc.localDescription.sdp,
      }),
    );

    $("rtcStateText").textContent = "Offer Sent...";
  } catch (e) {
    log(`Session Start Failed: ${e.message}`, "error");
    endSession();
  }
}

async function handleAnswer(msg) {
  if (!state.pc) return;
  try {
    await state.pc.setRemoteDescription({ type: "answer", sdp: msg.sdp });
    state.sessionId = msg.sessionId;
    $("sessionIdDisplay").textContent = state.sessionId;

    log(
      `Video Config: ${state.videoConfig.maxBitrate}kbps @ ${state.videoConfig.maxFramerate}fps, ${state.videoConfig.width}x${state.videoConfig.height}, constrained_baseline=${state.videoConfig.useConstrainedBaseline}`,
      "info",
    );

    // Apply video encoding constraints via RTCRtpSender
    await applyVideoConstraints();

    log("Session Established", "success");
    $("rtcStateText").textContent = "Active";
    setStep(2, "done");
    state.autoStartingSession = false;

    // Enable Call Controls (respects registration status)
    updateCallButtonState();
    
    // Auto-place queued call if any
    flushPendingCallQueue();
  } catch (e) {
    log("Error setting remote description: " + e.message, "error");
    state.autoStartingSession = false;
  }
}

// Apply video encoding constraints to limit bitrate and framerate
async function applyVideoConstraints() {
  if (!state.pc || !state.localStream) return;

  const videoSenders = state.pc.getSenders().filter((s) => s.track && s.track.kind === "video");

  for (const sender of videoSenders) {
    try {
      const params = sender.getParameters();
      if (!params.encodings) params.encodings = [{}];

      // Set max bitrate (convert kbps to bps)
      params.encodings[0].maxBitrate = state.videoConfig.maxBitrate * 1000;

      // Set max framerate
      params.encodings[0].maxFramerate = state.videoConfig.maxFramerate;

      await sender.setParameters(params);
      log(`Applied video constraints: maxBitrate=${state.videoConfig.maxBitrate}kbps, maxFramerate=${state.videoConfig.maxFramerate}fps`, "success");
    } catch (e) {
      log(`Could not apply video constraints: ${e.message}`, "warning");
    }
  }

  // Also apply video track constraints for resolution if different from current
  const videoTrack = state.localStream.getVideoTracks()[0];
  if (videoTrack) {
    try {
      const constraints = {
        width: { ideal: state.videoConfig.width },
        height: { ideal: state.videoConfig.height },
        frameRate: { max: state.videoConfig.maxFramerate },
      };
      await videoTrack.applyConstraints(constraints);
      log(
        `Applied video track constraints: ${state.videoConfig.width}x${state.videoConfig.height} @ ${state.videoConfig.maxFramerate}fps`,
        "success",
      );
    } catch (e) {
      log(`Could not apply video track constraints: ${e.message}`, "warning");
    }
  }
}

function endSession() {
  // Use hangup() instead to properly check call state
  hangup();
  cleanupSession();
}

function cleanupSession() {
  stopTimer();
  if (state.localStream) {
    state.localStream.getTracks().forEach((t) => t.stop());
    state.localStream = null;
  }
  if (state.pc) {
    state.pc.close();
    state.pc = null;
  }

  $("rtcStateText").textContent = "Not Ready";
  $("localVideo").srcObject = null;
  
  // Clear remote video properly to prevent frozen frame
  const remoteVideo = $("remoteVideo");
  if (remoteVideo) {
    remoteVideo.pause();
    remoteVideo.srcObject = null;
    remoteVideo.load();
  }
  
  setStep(2, "idle");

  $("btnCall").disabled = true;
  $("btnHangup").disabled = true;
  const btnOverlayHangup = $("btnOverlayHangup");
  if (btnOverlayHangup) btnOverlayHangup.disabled = true;
  state.sessionId = null;
  state.callState = null;
  state.activeCallSessionId = null;
  state.pendingCallRequest = null;
  state.autoStartingSession = false;
  stopStats();
  updateCallButtonState();
}

// --- Call State & Controls ---

// Handle call state changes
function handleCallState(callState) {
  log(`Call State: ${callState}`, "info");
  state.callState = callState; // Track call state
  
  const badge = $("callStateBadge");
  if (badge) {
    badge.textContent = callState.toUpperCase();
    // Update badge color based on state
    if (callState === "active") {
      badge.style.background = "rgba(16, 185, 129, 0.3)";
      badge.style.color = "#34d399";
      $("btnHangup").disabled = false;
      const btnOverlayHangup = $("btnOverlayHangup");
      if (btnOverlayHangup) btnOverlayHangup.disabled = false;
      state.activeCallSessionId = state.sessionId; // Set active call session
      startTimer();
    } else if (callState === "ringing" || callState === "connecting") {
      badge.style.background = "rgba(245, 158, 11, 0.3)";
      badge.style.color = "#fbbf24";
      $("btnHangup").disabled = false;
      const btnOverlayHangup = $("btnOverlayHangup");
      if (btnOverlayHangup) btnOverlayHangup.disabled = false;
      state.activeCallSessionId = state.sessionId; // Set active call session
    } else if (callState === "ended") {
      badge.style.background = "rgba(239, 68, 68, 0.3)";
      badge.style.color = "#fca5a5";
      $("btnHangup").disabled = true;
      const btnOverlayHangup = $("btnOverlayHangup");
      if (btnOverlayHangup) btnOverlayHangup.disabled = true;
      state.activeCallSessionId = null; // Clear active call session
      stopTimer();
      // Cleanup session when call ends (server deletes session on hangup)
      cleanupSession();
    }
  }
  updateCallButtonState();
}

// Hangup call
function hangup() {
  // Check if we have an active call before sending hangup
  if (!state.activeCallSessionId || !state.ws) {
    log("No active call to hangup", "warning");
    return;
  }
  
  // Only send hangup if call is in active state
  if (!state.callState || state.callState === "ended") {
    log("Call already ended, skipping hangup", "info");
    return;
  }
  
  log(`Sending hangup for sessionId: ${state.activeCallSessionId}`, "info");
  state.ws.send(JSON.stringify({ type: "hangup", sessionId: state.activeCallSessionId }));
  log("Hangup request sent", "info");
}

// Send DTMF digits
function sendDTMF(digits) {
  if (!state.sessionId || !state.ws) {
    log("No active call for DTMF", "warning");
    return;
  }
  state.ws.send(JSON.stringify({ type: "dtmf", sessionId: state.sessionId, digits }));
  log(`DTMF sent: ${digits}`, "info");
}

// Handle received DTMF from remote
function handleReceivedDTMF(msg) {
  log(`DTMF received: ${msg.digits}`, "info");
}

// Handle incoming call
function handleIncomingCall(msg) {
  log(`Incoming call from ${msg.from} (to: ${msg.to || 'unknown'})`, "info");
  
  // Set sessionId from incoming event if present
  if (msg.sessionId) {
    state.sessionId = msg.sessionId;
    $("sessionIdDisplay").textContent = state.sessionId;
  }
  
  const modal = $("incomingCallModal");
  const callerID = $("incomingCallerID");
  const callerTo = $("incomingCallerTo");
  const callMode = $("incomingCallMode");
  
  if (modal && callerID) {
    callerID.textContent = msg.from || "Unknown";
    if (callerTo) callerTo.textContent = msg.to || "Unknown";
    if (callMode) callMode.textContent = state.mode.toUpperCase();
    modal.classList.remove("hidden");
    modal.style.display = "flex";
  }
}

// Accept incoming call
function acceptCall() {
  const modal = $("incomingCallModal");
  if (modal) {
    modal.classList.add("hidden");
    modal.style.display = "none";
  }
  if (!state.sessionId || !state.ws) {
    log("Cannot accept call - no session", "error");
    return;
  }
  state.ws.send(JSON.stringify({ type: "accept", sessionId: state.sessionId }));
  log("Call accepted", "success");
}

// Reject incoming call
function rejectCall() {
  const modal = $("incomingCallModal");
  if (modal) {
    modal.classList.add("hidden");
    modal.style.display = "none";
  }
  if (!state.sessionId || !state.ws) {
    log("Cannot reject call - no session", "error");
    return;
  }
  state.ws.send(JSON.stringify({ type: "reject", sessionId: state.sessionId, reason: "decline" }));
  log("Call rejected", "info");
}

// Toggle audio mute
function toggleMuteAudio() {
  if (!state.localStream) return;
  state.isMutedAudio = !state.isMutedAudio;
  state.localStream.getAudioTracks().forEach((track) => {
    track.enabled = !state.isMutedAudio;
  });
  const btn = $("btnMuteAudio");
  if (btn) {
    btn.textContent = state.isMutedAudio ? "Unmute" : "Mic";
    btn.style.background = state.isMutedAudio ? "rgba(239, 68, 68, 0.5)" : "";
  }
  log(state.isMutedAudio ? "Audio muted" : "Audio unmuted", "info");
}

// Toggle video mute
function toggleMuteVideo() {
  if (!state.localStream) return;
  state.isMutedVideo = !state.isMutedVideo;
  state.localStream.getVideoTracks().forEach((track) => {
    track.enabled = !state.isMutedVideo;
  });
  const btn = $("btnMuteVideo");
  if (btn) {
    btn.textContent = state.isMutedVideo ? "Start Video" : "Cam";
    btn.style.background = state.isMutedVideo ? "rgba(239, 68, 68, 0.5)" : "";
  }
  log(state.isMutedVideo ? "Video muted" : "Video unmuted", "info");
}

// Toggle stats overlay
function toggleStats() {
  const overlay = $("statsOverlay");
  if (overlay) {
    const isVisible = overlay.style.display !== "none";
    overlay.style.display = isVisible ? "none" : "block";
    if (!isVisible) {
      startStats();
    } else {
      stopStats();
    }
  }
}

// Start stats collection
function startStats() {
  if (state.statsInterval) return;
  state.statsInterval = setInterval(async () => {
    if (!state.pc) return;
    const stats = await state.pc.getStats();
    stats.forEach((report) => {
      if (report.type === "inbound-rtp" && report.kind === "video") {
        const rtt = $("statRTT");
        const loss = $("statLoss");
        const bitrate = $("statBitrate");
        const codec = $("statCodec");
        const res = $("statRes");
        if (rtt) rtt.textContent = report.roundTripTime ? (report.roundTripTime * 1000).toFixed(0) : "-";
        if (loss) loss.textContent = report.packetsLost ? ((report.packetsLost / report.packetsReceived) * 100).toFixed(1) : "0";
        if (bitrate && report.bytesReceived) {
          const kbps = ((report.bytesReceived * 8) / 1000).toFixed(0);
          bitrate.textContent = kbps;
        }
        if (codec) codec.textContent = report.codecId || "-";
        if (res) res.textContent = report.frameWidth && report.frameHeight ? `${report.frameWidth}x${report.frameHeight}` : "-";
      }
    });
  }, 1000);
}

// Stop stats collection
function stopStats() {
  if (state.statsInterval) {
    clearInterval(state.statsInterval);
    state.statsInterval = null;
  }
}

// Start call timer
function startTimer() {
  stopTimer(); // Clear any existing timer
  state.callStartTime = Date.now();
  state.callTimerInterval = setInterval(() => {
    const elapsed = Math.floor((Date.now() - state.callStartTime) / 1000);
    const minutes = Math.floor(elapsed / 60).toString().padStart(2, "0");
    const seconds = (elapsed % 60).toString().padStart(2, "0");
    const timerEl = $("callTimer");
    if (timerEl) timerEl.textContent = `${minutes}:${seconds}`;
  }, 1000);
}

// Stop call timer
function stopTimer() {
  if (state.callTimerInterval) {
    clearInterval(state.callTimerInterval);
    state.callTimerInterval = null;
    state.callStartTime = null;
    const timerEl = $("callTimer");
    if (timerEl) timerEl.textContent = "00:00";
  }
}

// --- Call Logic ---

// 3. Call Logic
function makeCall() {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    log("WebSocket not connected", "error");
    return;
  }

  const dest = $("destination").value;
  if (!dest) {
    log("Please enter a destination", "error");
    return;
  }
  
  let callParams = {
    destination: dest,
  };
  
  // Build call params based on mode
  if (state.mode === 'siptrunk') {
    const trunkIdInput = $("trunkId");
    const trunkId = trunkIdInput ? parseInt(trunkIdInput.value || "0") : 0;
    
    if (Number.isNaN(trunkId) || trunkId <= 0) {
      log("Trunk ID required in SIP Trunk mode", "error");
      return;
    }
    
    callParams.trunkId = trunkId;
  } else {
    // Public mode
    const sipDomain = $("sipDomain").value.trim();
    const sipUsername = $("sipUsername").value.trim();
    const sipPassword = $("sipPassword").value;
    const sipPort = parseInt($("sipPort").value) || 5060;
    
    if (!sipDomain || !sipUsername || !sipPassword) {
      log("SIP credentials required in Public mode", "error");
      return;
    }
    
    callParams.sipDomain = sipDomain;
    callParams.sipUsername = sipUsername;
    callParams.sipPassword = sipPassword;
    callParams.sipPort = sipPort;
  }

  // If no session exists, queue call and auto-start session
  if (!state.sessionId) {
    state.pendingCallRequest = callParams;
    log("Media session not ready - preparing automatically before placing call...", "info");
    ensureMediaSessionForCall();
    return;
  }

  sendCallPayload(callParams);
}

function ensureMediaSessionForCall() {
  if (state.autoStartingSession || state.pc) {
    return;
  }

  state.autoStartingSession = true;
  startSession().catch((e) => {
    state.autoStartingSession = false;
    state.pendingCallRequest = null;
    log(`Failed to start media session automatically: ${e.message}`, "error");
    updateCallButtonState();
  });
}

function sendCallPayload(params) {
  if (!state.sessionId) {
    log("Cannot place call without active session", "error");
    return;
  }

  state.callCount++;
  log(`Calling ${params.destination}... (Call #${state.callCount} in this browser session, mode: ${state.mode})`, "info");

  const payload = {
    type: "call",
    sessionId: state.sessionId,
    destination: params.destination,
  };

  if (params.trunkId) {
    payload.trunkId = params.trunkId;
  } else {
    payload.sipDomain = params.sipDomain;
    payload.sipUsername = params.sipUsername;
    payload.sipPassword = params.sipPassword;
    payload.sipPort = params.sipPort;
  }

  state.ws.send(JSON.stringify(payload));
  localStorage.setItem("k2_last_dest", params.destination);
}

function flushPendingCallQueue() {
  if (!state.pendingCallRequest || !state.sessionId) {
    return;
  }

  log("Auto-placing queued call...", "info");
  const params = state.pendingCallRequest;
  state.pendingCallRequest = null;
  sendCallPayload(params);
}

// --- Trunk Helpers ---

// Trunk resolve handlers
function handleTrunkResolved(msg) {
  state.trunkResolvePending = false;
  state.trunkId = msg.trunkId;
  
  const trunkIdInput = $("trunkId");
  const trunkStatusBadge = $("trunkStatusBadge");
  const btnResolveTrunk = $("btnResolveTrunk");
  const btnSendMessage = $("btnSendMessage");
  
  if (trunkIdInput) trunkIdInput.value = msg.trunkId || "";
  log(`Trunk resolved: ID ${msg.trunkId}`, "success");
  if (trunkStatusBadge) {
    trunkStatusBadge.textContent = "Resolved";
    trunkStatusBadge.style.background = "rgba(16, 185, 129, 0.3)";
    trunkStatusBadge.style.color = "#34d399";
  }
  if (btnResolveTrunk) btnResolveTrunk.disabled = false;
  if (btnSendMessage) btnSendMessage.disabled = false;
  updateCallButtonState();
}

function handleTrunkRedirect(msg) {
  state.trunkResolvePending = true;
  const trunkStatusBadge = $("trunkStatusBadge");
  const btnResolveTrunk = $("btnResolveTrunk");
  
  if (trunkStatusBadge) {
    trunkStatusBadge.textContent = "Redirecting...";
    trunkStatusBadge.style.background = "rgba(245, 158, 11, 0.3)";
    trunkStatusBadge.style.color = "#fbbf24";
  }
  if (btnResolveTrunk) btnResolveTrunk.disabled = true;

  if (!msg.redirectUrl) {
    log("Redirect URL missing", "error");
    return;
  }

  log(`Redirecting to ${msg.redirectUrl}`, "warning");
  state.pendingRedirectUrl = msg.redirectUrl;
  if (state.ws) {
    state.ws.close();
  } else {
    connect(msg.redirectUrl);
  }
}

function handleTrunkNotFound(msg) {
  state.trunkResolvePending = false;
  log(`Trunk not found: ${msg.reason || "No match"}`, "error");
  
  const trunkStatusBadge = $("trunkStatusBadge");
  const btnResolveTrunk = $("btnResolveTrunk");
  const btnSendMessage = $("btnSendMessage");
  
  if (trunkStatusBadge) {
    trunkStatusBadge.textContent = "Not Found";
    trunkStatusBadge.style.background = "rgba(239, 68, 68, 0.3)";
    trunkStatusBadge.style.color = "#fca5a5";
  }
  if (btnResolveTrunk) btnResolveTrunk.disabled = false;
  if (btnSendMessage) btnSendMessage.disabled = true;
  updateCallButtonState();
}

function handleTrunkNotReady(msg) {
  state.trunkResolvePending = false;
  log(`Trunk not ready: ${msg.reason || "Owner not discoverable"}`, "warning");
  
  const trunkStatusBadge = $("trunkStatusBadge");
  const btnResolveTrunk = $("btnResolveTrunk");
  const btnSendMessage = $("btnSendMessage");
  
  if (trunkStatusBadge) {
    trunkStatusBadge.textContent = "Not Ready";
    trunkStatusBadge.style.background = "rgba(245, 158, 11, 0.3)";
    trunkStatusBadge.style.color = "#fbbf24";
  }
  if (btnResolveTrunk) btnResolveTrunk.disabled = false;
  if (btnSendMessage) btnSendMessage.disabled = true;
  updateCallButtonState();
}

// Update trunk UI when WebSocket connects/disconnects
function updateTrunkUI(connected) {
  // Skip if trunk UI elements don't exist (public-only mode)
  const btnResolveTrunk = $("btnResolveTrunk");
  const trunkStatusBadge = $("trunkStatusBadge");
  const trunkIdInput = $("trunkId");
  const btnSendMessage = $("btnSendMessage");
  
  if (!btnResolveTrunk || !trunkStatusBadge || !trunkIdInput) {
    return; // Trunk UI not present, skip
  }

  if (connected) {
    // Enable resolve button if credentials are filled
    const domain = $("trunkSipDomain") ? $("trunkSipDomain").value.trim() : "";
    const username = $("trunkSipUsername") ? $("trunkSipUsername").value.trim() : "";
    const password = $("trunkSipPassword") ? $("trunkSipPassword").value : "";
    btnResolveTrunk.disabled = !(domain && username && password);
  } else {
    btnResolveTrunk.disabled = true;
    state.trunkResolvePending = false;
    state.trunkId = null;
    trunkIdInput.value = "";
    trunkStatusBadge.textContent = "Not Resolved";
    trunkStatusBadge.style.background = "rgba(239, 68, 68, 0.3)";
    trunkStatusBadge.style.color = "#fca5a5";
    if (btnSendMessage) btnSendMessage.disabled = true;
  }
}

// Resolve Trunk from UI (for trunk mode)
function resolveTrunkFromUI() {
  const domain = $("trunkSipDomain").value.trim();
  const username = $("trunkSipUsername").value.trim();
  const password = $("trunkSipPassword").value;
  const port = parseInt($("trunkSipPort").value) || 5060;

  if (!domain || !username || !password) {
    log("Please fill in all trunk credentials", "error");
    return;
  }

  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    log("WebSocket not connected", "error");
    return;
  }

  state.trunkResolvePayload = { sipDomain: domain, sipUsername: username, sipPassword: password, sipPort: port };
  state.trunkResolvePending = true;

  log(`Resolving trunk: ${username}@${domain}:${port}...`, "info");
  
  const btnResolveTrunk = $("btnResolveTrunk");
  const trunkStatusBadge = $("trunkStatusBadge");
  
  if (btnResolveTrunk) btnResolveTrunk.disabled = true;
  if (trunkStatusBadge) {
    trunkStatusBadge.textContent = "Resolving...";
    trunkStatusBadge.style.background = "rgba(245, 158, 11, 0.3)";
    trunkStatusBadge.style.color = "#fbbf24";
  }

  state.ws.send(
    JSON.stringify({
      type: "trunk_resolve",
      sipDomain: domain,
      sipUsername: username,
      sipPassword: password,
      sipPort: port,
    }),
  );
}

// Resolve Trunk (legacy function - kept for compatibility)
function resolveTrunk() {
  resolveTrunkFromUI();
}

// Update call button state based on session and SIP credentials
function updateCallButtonState() {
  const wsReady = state.ws && state.ws.readyState === WebSocket.OPEN;
  
  let credentialReady = false;
  
  if (state.mode === 'siptrunk') {
    const trunkIdInput = $("trunkId");
    const trunkId = trunkIdInput ? parseInt(trunkIdInput.value || "0") : 0;
    credentialReady = !Number.isNaN(trunkId) && trunkId > 0;
  } else {
    // Public mode
    const sipDomain = $("sipDomain").value.trim();
    const sipUsername = $("sipUsername").value.trim();
    const sipPassword = $("sipPassword").value;
    credentialReady = sipDomain && sipUsername && sipPassword;
  }
  
  // Enable Call button if:
  // - WebSocket is ready
  // - Credentials are ready
  // - Not currently in a call (callState is not connecting/ringing/active)
  // - Not currently auto-starting a session
  const inCall = state.callState === "connecting" || state.callState === "ringing" || state.callState === "active";
  const ready = wsReady && credentialReady && !inCall && !state.autoStartingSession;
  $("btnCall").disabled = !ready;
}

// --- SIP Message Handling ---

// Handle incoming SIP message
function handleIncomingSIPMessage(msg) {
  log(`Message from ${msg.from}: ${msg.body}`, "success");

  const container = $("messageContainer");
  const div = document.createElement("div");
  div.className = "log-entry success";
  const time = new Date().toLocaleTimeString("en-US", { hour12: false });
  div.innerHTML = `<span class="time">${time}</span><b>${msg.from}:</b> ${msg.body}`;
  container.appendChild(div);
  container.scrollTop = container.scrollHeight;
}

// Send SIP message
function sendSIPMessage() {
  const body = $("msgBody").value.trim();

  if (!body) {
    log("Please enter a message", "error");
    return;
  }

  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    log("WebSocket not connected", "error");
    return;
  }

  // Server will automatically route to the correct destination via session
  state.ws.send(
    JSON.stringify({
      type: "send_message",
      destination: "", // Server handles routing via active session
      body: body,
      contentType: "text/plain;charset=UTF-8",
    }),
  );

  // Show sent message in UI
  const container = $("messageContainer");
  const div = document.createElement("div");
  div.className = "log-entry info";
  const time = new Date().toLocaleTimeString("en-US", { hour12: false });
  div.innerHTML = `<span class="time">${time}</span><b>You:</b> ${body}`;
  container.appendChild(div);
  container.scrollTop = container.scrollHeight;

  // Clear input
  $("msgBody").value = "";
}

// Clear messages
function clearMessages() {
  $("messageContainer").innerHTML = "";
}

// Initialize
window.onload = () => {
  const lastDest = localStorage.getItem("k2_last_dest");
  if (lastDest) $("destination").value = lastDest;
  
  // Load saved mode
  const savedMode = localStorage.getItem("k2_mode") || "public";
  switchMode(savedMode);
  
  $("destination").addEventListener("input", () => {
    updateCallButtonState();
  });
  
  // Public mode fields
  ["sipDomain", "sipUsername", "sipPassword", "sipPort"].forEach((id) => {
    const el = $(id);
    if (el) el.addEventListener("input", () => updateCallButtonState());
  });
  
  // Trunk mode fields
  const trunkIdInput = $("trunkId");
  if (trunkIdInput) {
    trunkIdInput.addEventListener("input", () => updateCallButtonState());
  }
  
  ["trunkSipDomain", "trunkSipUsername", "trunkSipPassword", "trunkSipPort"].forEach((id) => {
    const el = $(id);
    if (el) el.addEventListener("input", () => {
      // Enable resolve button if all trunk credentials are filled
      const domain = $("trunkSipDomain").value.trim();
      const username = $("trunkSipUsername").value.trim();
      const password = $("trunkSipPassword").value;
      const btnResolveTrunk = $("btnResolveTrunk");
      if (btnResolveTrunk && state.ws && state.ws.readyState === WebSocket.OPEN) {
        btnResolveTrunk.disabled = !(domain && username && password);
      }
    });
  });

  log("Ready to connect.");
};
