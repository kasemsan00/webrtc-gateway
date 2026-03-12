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
  isMutedAudio: false,
  isMutedVideo: false,
  statsInterval: null,
  callTimerInterval: null,
  callStartTime: null,
  callCount: 0, // Track number of calls for debugging video issues
  isRegistered: false, // Track SIP registration status
  pingInterval: null, // Auto-ping interval
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

// --- Logic ---

// 1. WebSocket
function toggleConnect() {
  if (state.ws) {
    state.ws.close();
  } else {
    connect();
  }
}

function connect() {
  log(`Connecting to ${CONFIG.wssUrl}...`);
  $("wsStateText").textContent = "Connecting...";

  try {
    state.ws = new WebSocket(CONFIG.wssUrl);

    state.ws.onopen = () => {
      log("WebSocket Connected", "success");
      $("wsStateText").textContent = "Connected";
      $("connectionStatusDot").className = "status-dot connected";
      setStep(1, "done");
      setStep(2, "active"); // Ready for next step
      $("btnStartSession").disabled = false;
      updateSIPUI(true); // Enable SIP registration
      startPingInterval(); // Start auto-ping
    };

    state.ws.onclose = () => {
      log("WebSocket Disconnected", "warning");
      $("wsStateText").textContent = "Disconnected";
      $("connectionStatusDot").className = "status-dot disconnected";
      cleanupSession();
      setStep(1, "idle");
      setStep(2, "idle");
      $("btnStartSession").disabled = true;
      updateSIPUI(false); // Disable SIP registration
      stopPingInterval(); // Stop auto-ping
      state.ws = null;
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
      if (msg.sessionId) {
        state.sessionId = msg.sessionId;
        $("sessionIdDisplay").textContent = state.sessionId;
      }
      handleCallState(msg.state);
      break;
    case "incoming":
      handleIncomingCall(msg);
      break;
    case "registerStatus":
      handleRegisterStatus(msg);
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
      })
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
    "info"
    );

    // Apply video encoding constraints via RTCRtpSender
    await applyVideoConstraints();

    log("Session Established", "success");
    $("rtcStateText").textContent = "Active";
    setStep(2, "done");

    // Enable Call Controls (respects registration status)
    updateCallButtonState();
  } catch (e) {
    log("Error setting remote description: " + e.message, "error");
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
        "success"
      );
    } catch (e) {
      log(`Could not apply video track constraints: ${e.message}`, "warning");
    }
  }
}

function endSession() {
  if (state.sessionId && state.ws) {
    state.ws.send(JSON.stringify({ type: "hangup", sessionId: state.sessionId }));
  }
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
  $("remoteVideo").srcObject = null;
  setStep(2, "idle");

  $("btnCall").disabled = true;
  $("btnHangup").disabled = true;
  state.sessionId = null;
  stopStats();
}

// 3. Call Logic
function makeCall() {
  const dest = $("destination").value;
  if (!dest || !state.sessionId) return;

  state.callCount++;
  log(`Calling ${dest}... (Call #${state.callCount} in this browser session)`, "info");
  state.ws.send(
    JSON.stringify({
      type: "call",
      sessionId: state.sessionId,
      destination: dest,
    })
  );

  // Save to localstorage
  localStorage.setItem("k2_last_dest", dest);
}

function hangup() {
  if (!state.sessionId) return;
  log("Hanging up...", "warning");
  state.ws.send(
    JSON.stringify({
      type: "hangup",
      sessionId: state.sessionId,
    })
  );
}

function startTimer() {
  stopTimer();
  state.callStartTime = Date.now();
  $("callTimer").style.color = "var(--text-main)";

  state.callTimerInterval = setInterval(() => {
    const now = Date.now();
    const diff = Math.floor((now - state.callStartTime) / 1000);
    const m = Math.floor(diff / 60)
      .toString()
      .padStart(2, "0");
    const s = (diff % 60).toString().padStart(2, "0");
    $("callTimer").textContent = `${m}:${s}`;
  }, 1000);
}

function stopTimer() {
  if (state.callTimerInterval) {
    clearInterval(state.callTimerInterval);
    state.callTimerInterval = null;
  }
  $("callTimer").textContent = "00:00";
  $("callTimer").style.color = "var(--text-muted)";
}

function handleCallState(s) {
  const badge = $("callStateBadge");
  badge.textContent = s.toUpperCase();

  if (s === "active" || s === "answered") {
    badge.style.background = "var(--success)";
    $("btnCall").disabled = true;
    $("btnHangup").disabled = false;
    startStats();
    startTimer();
  } else if (s === "ringing" || s === "trying") {
    badge.style.background = "var(--warning)";
    $("btnCall").disabled = true;
    $("btnHangup").disabled = false;
  } else if (s === "ended" || s === "failed") {
    badge.style.background = "var(--danger)";
    $("btnHangup").disabled = true;
    stopStats();
    stopTimer();

    // Clear remote video and audio to prevent frozen display
    // Note: Stop tracks from old streams before clearing srcObject
    const remoteVideoEl = $("remoteVideo");
    const remoteAudioEl = $("remoteAudio");

    if (remoteVideoEl.srcObject) {
      remoteVideoEl.srcObject.getTracks().forEach((t) => t.stop());
      remoteVideoEl.srcObject = null;
    }
    if (remoteAudioEl.srcObject) {
      remoteAudioEl.srcObject.getTracks().forEach((t) => t.stop());
      remoteAudioEl.srcObject = null;
    }
    log("Remote streams cleared and stopped", "info");

    // Session was deleted on server, need to create new one
    // Close current PeerConnection and create new session automatically
    log("Session ended - recreating session for next call...", "info");

    // Close old PeerConnection but keep local stream
    if (state.pc) {
      state.pc.close();
      state.pc = null;
    }
    state.sessionId = null;
    $("sessionIdDisplay").textContent = "-";
    $("iceStateDisplay").textContent = "New";
    $("sigStateDisplay").textContent = "Stable";
    $("btnCall").disabled = true; // Disable until new session is ready

    // Reset badge to IDLE after a delay (show ended briefly)
    setTimeout(() => {
      if (!state.sessionId) {
        badge.textContent = "IDLE";
        badge.style.background = "rgba(255, 255, 255, 0.1)";
      }
    }, 1500);

    // Automatically recreate session if WebSocket is still connected
    if (state.ws && state.ws.readyState === WebSocket.OPEN && state.localStream) {
      setTimeout(() => {
        recreateSession();
      }, 500); // Small delay before recreating
    }
  }

  log(`Call State: ${s.toUpperCase()}`);
}

// Recreate session after hangup (keeps local stream, creates new PeerConnection)
async function recreateSession() {
  try {
    log("Creating new PeerConnection for next call...");
    $("rtcStateText").textContent = "Reconnecting...";

    // Ensure no old PeerConnection is lingering
    if (state.pc) {
      log("Warning: PeerConnection still exists, closing...", "warning");
      state.pc.close();
      state.pc = null;
    }

    // Init PC
    state.pc = new RTCPeerConnection({
      iceServers: [
        {
          urls: "turn:turn.ttrs.or.th:3478?transport=udp",
          username: "turn01",
          credential: "Test1234",
        },
      ],
    });

    // Store reference for closures
    const pc = state.pc;

    // ICE Events
    pc.oniceconnectionstatechange = () => {
      if (state.pc !== pc) return; // Ignore if PeerConnection changed
      const s = pc.iceConnectionState;
      log(`ICE State: ${s}`, s === "connected" ? "success" : "info");
      $("iceStateDisplay").textContent = s;
      if (s === "disconnected" || s === "failed") {
        $("connectionStatusDot").className = "status-dot disconnected";
      }
    };

    pc.onsignalingstatechange = () => {
      if (state.pc !== pc) return; // Ignore if PeerConnection changed
      $("sigStateDisplay").textContent = pc.signalingState;
    };

    // Tracks - add local tracks to new PeerConnection
    state.localStream.getTracks().forEach((track) => pc.addTrack(track, state.localStream));

    // ontrack - receive remote tracks (from server/SIP)
    pc.ontrack = (event) => {
      if (state.pc !== pc) {
        log(`Ignoring ontrack from old PeerConnection`, "warning");
        return; // Ignore if PeerConnection changed (old session)
      }

      log(`Remote Track received: ${event.track.kind} (id: ${event.track.id})`, "success");

      if (event.track.kind === "video") {
        const stream = event.streams[0];
        log(`Assigning video stream (id: ${stream ? stream.id : "null"}) to remoteVideo`, "info");
        $("remoteVideo").srcObject = stream;
      }
      if (event.track.kind === "audio") {
        const stream = event.streams[0];
        log(`Assigning audio stream (id: ${stream ? stream.id : "null"}) to remoteAudio`, "info");
        $("remoteAudio").srcObject = stream;
      }
    };

    // Transceiver Codec preferences (Force H.264 only, let browser negotiate audio codec)
    const transceivers = pc.getTransceivers();
    transceivers.forEach((t) => {
      if (t.receiver && t.receiver.track && t.receiver.track.kind === "video") {
        const codecs = RTCRtpSender.getCapabilities("video").codecs;
        let h264Codecs = codecs.filter((c) => c.mimeType === "video/H264" && c.sdpFmtpLine && c.sdpFmtpLine.includes("packetization-mode=1"));

        // If constrained baseline is requested, filter for profile-level-id 42e0xx (Constrained Baseline)
        if (state.videoConfig.useConstrainedBaseline) {
          const constrainedBaseline = h264Codecs.filter((c) => c.sdpFmtpLine && c.sdpFmtpLine.includes("profile-level-id=42e0"));
          if (constrainedBaseline.length > 0) {
            h264Codecs = constrainedBaseline;
            log("Using H.264 Constrained Baseline Profile (Recreate)", "info");
          } else {
            log("H.264 Constrained Baseline not available, using default H.264", "warning");
          }
        }

        if (h264Codecs.length) t.setCodecPreferences(h264Codecs);
      }
      // Audio: No codec preference - let browser negotiate (Opus, PCMU, etc.)
    });

    // Offer
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);

    // Wait for ICE Gathering
    if (pc.iceGatheringState !== "complete") {
      await new Promise((r) => {
        let timeout = setTimeout(r, 2000);
        pc.onicegatheringstatechange = () => {
          if (pc.iceGatheringState === "complete") {
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
        sdp: pc.localDescription.sdp,
      })
    );

    $("rtcStateText").textContent = "Offer Sent...";
    log("Offer sent to server, waiting for answer...", "info");
  } catch (e) {
    log(`Failed to recreate session: ${e.message}`, "error");
    $("rtcStateText").textContent = "Error - Click Start";
    $("btnCall").disabled = true;
  }
}

function sendDTMF(digit) {
  if (!state.sessionId) return;
  state.ws.send(
    JSON.stringify({
      type: "dtmf",
      sessionId: state.sessionId,
      digits: digit,
    })
  );
  log(`DTMF Sent: ${digit}`);

  // Visual feedback - flash the dialpad button
  flashDialpadButton(digit);
}

// Handle received DTMF from SIP peer (RFC 2833)
function handleReceivedDTMF(msg) {
  const digit = msg.digits || msg.digit;
  log(`DTMF Received: ${digit}`, "success");

  // Visual feedback - flash the dialpad button
  flashDialpadButton(digit);

  // Optional: Play DTMF tone for audio feedback
  playDTMFTone(digit);
}

// Flash dialpad button for visual feedback
function flashDialpadButton(digit) {
  // Find the button with matching digit
  const buttons = document.querySelectorAll("#dialpad button");
  buttons.forEach((btn) => {
    if (btn.textContent.trim() === digit) {
      btn.classList.add("dtmf-flash");
      setTimeout(() => btn.classList.remove("dtmf-flash"), 200);
    }
  });
}

// Play DTMF tone (simple Web Audio API implementation)
function playDTMFTone(digit) {
  const dtmfFreqs = {
    1: [697, 1209],
    2: [697, 1336],
    3: [697, 1477],
    4: [770, 1209],
    5: [770, 1336],
    6: [770, 1477],
    7: [852, 1209],
    8: [852, 1336],
    9: [852, 1477],
    "*": [941, 1209],
    0: [941, 1336],
    "#": [941, 1477],
    A: [697, 1633],
    B: [770, 1633],
    C: [852, 1633],
    D: [941, 1633],
  };

  const freqs = dtmfFreqs[digit];
  if (!freqs) return;

  try {
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    const duration = 0.15; // 150ms
    const gain = ctx.createGain();
    gain.gain.value = 0.1; // Low volume
    gain.connect(ctx.destination);

    freqs.forEach((freq) => {
      const osc = ctx.createOscillator();
      osc.type = "sine";
      osc.frequency.value = freq;
      osc.connect(gain);
      osc.start();
      osc.stop(ctx.currentTime + duration);
    });
  } catch (e) {
    // Ignore audio errors (user may not have interacted with page yet)
  }
}

// 4. In-Call Utilities
function toggleMuteAudio() {
  if (!state.localStream) return;
  state.isMutedAudio = !state.isMutedAudio;
  state.localStream.getAudioTracks().forEach((t) => (t.enabled = !state.isMutedAudio));

  const btn = $("btnMuteAudio");
  if (state.isMutedAudio) {
    btn.classList.add("active");
    btn.innerHTML = "🔇";
  } else {
    btn.classList.remove("active");
    btn.innerHTML = "🎤";
  }
}

function toggleMuteVideo() {
  if (!state.localStream) return;
  state.isMutedVideo = !state.isMutedVideo;
  state.localStream.getVideoTracks().forEach((t) => (t.enabled = !state.isMutedVideo));

  const btn = $("btnMuteVideo");
  if (state.isMutedVideo) {
    btn.classList.add("active");
    btn.innerHTML = "🚫";
  } else {
    btn.classList.remove("active");
    btn.innerHTML = "📷";
  }
}

function toggleStats() {
  const el = $("statsOverlay");
  el.style.display = el.style.display === "none" ? "block" : "none";
}

function startStats() {
  if (state.statsInterval) clearInterval(state.statsInterval);
  state.statsInterval = setInterval(async () => {
    if (!state.pc) return;
    const stats = await state.pc.getStats();
    let activeCandidatePair = null;

    stats.forEach((report) => {
      if (report.type === "transport") {
        // could find selected candidate pair here
      }
      if (report.type === "candidate-pair" && report.state === "succeeded") {
        activeCandidatePair = report;
        $("statRTT").textContent = (report.currentRoundTripTime * 1000).toFixed(0);
      }
      if (report.type === "inbound-rtp" && report.kind === "video") {
        $("statLoss").textContent = ((report.packetsLost / report.packetsReceived) * 100).toFixed(2);
      }
      if (report.type === "outbound-rtp" && report.kind === "video") {
        // rough estimation
      }
    });
  }, 1000);
}

function stopStats() {
  if (state.statsInterval) clearInterval(state.statsInterval);
}

// --- Incoming Call Handling ---

// Store incoming call info
let pendingIncomingSessionId = null;

// Handle incoming call notification from server
function handleIncomingCall(msg) {
  log(`Incoming Call from: ${msg.from}`, "warning");

  // Store the incoming session ID
  pendingIncomingSessionId = msg.sessionId;

  // Update UI - show modal using class toggle
  $("incomingCallerID").textContent = msg.from || "Unknown";
  const modal = $("incomingCallModal");
  modal.classList.remove("hidden");
  modal.classList.add("visible");

  // Play ring tone (optional)
  // You can add audio element for ringing sound here
}

async function acceptCall() {
  if (!pendingIncomingSessionId) {
    log("No pending incoming call", "error");
    return;
  }

  log("Accepting incoming call...", "success");
  const modal = $("incomingCallModal");
  modal.classList.remove("visible");
  modal.classList.add("hidden");

  try {
    // If we don't have a session yet, start one
    if (!state.pc) {
      await startSession();
    }

    // Send accept message - server expects sessionId to be the INCOMING SIP session
    // Server will use client.sessionID (from the offer/answer) to find WebRTC session
    state.ws.send(
      JSON.stringify({
        type: "accept",
        sessionId: pendingIncomingSessionId, // Send the incoming SIP session ID
      })
    );

    pendingIncomingSessionId = null;
  } catch (e) {
    log(`Error accepting call: ${e.message}`, "error");
  }
}

function rejectCall() {
  if (!pendingIncomingSessionId) {
    log("No pending incoming call", "error");
    return;
  }

  log("Rejecting incoming call", "warning");
  const modal = $("incomingCallModal");
  modal.classList.remove("visible");
  modal.classList.add("hidden");

  state.ws.send(
    JSON.stringify({
      type: "reject",
      sessionId: pendingIncomingSessionId,
    })
  );

  pendingIncomingSessionId = null;
}

// --- SIP Registration ---

// Register with SIP server
function registerSIP() {
  const domain = $("sipDomain").value.trim();
  const username = $("sipUsername").value.trim();
  const password = $("sipPassword").value;
  const port = parseInt($("sipPort").value) || 5060;

  if (!domain || !username || !password) {
    log("Please fill in all SIP credentials", "error");
    return;
  }

  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    log("WebSocket not connected", "error");
    return;
  }

  log(`Registering to SIP: ${username}@${domain}:${port}...`, "info");
  $("btnRegister").disabled = true;
  $("sipStatusBadge").textContent = "Registering...";
  $("sipStatusBadge").style.background = "rgba(245, 158, 11, 0.3)";
  $("sipStatusBadge").style.color = "#fbbf24";

  state.ws.send(
    JSON.stringify({
      type: "register",
      sipDomain: domain,
      sipUsername: username,
      sipPassword: password,
      sipPort: port,
    })
  );
}

// Unregister from SIP server
function unregisterSIP() {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    log("WebSocket not connected", "error");
    return;
  }

  log("Unregistering from SIP server...", "info");
  $("btnUnregister").disabled = true;

  state.ws.send(
    JSON.stringify({
      type: "unregister",
    })
  );
}

// Handle registration status from server
function handleRegisterStatus(msg) {
  state.isRegistered = msg.registered;

  if (msg.registered) {
    log(`SIP Registered: ${msg.sipDomain || ""}`, "success");
    $("sipStatusBadge").textContent = "Registered";
    $("sipStatusBadge").style.background = "rgba(16, 185, 129, 0.3)";
    $("sipStatusBadge").style.color = "#34d399";
    $("btnRegister").disabled = true;
    $("btnUnregister").disabled = false;
    $("btnSendMessage").disabled = false;
  } else {
    log("SIP Unregistered", "info");
    $("sipStatusBadge").textContent = "Not Registered";
    $("sipStatusBadge").style.background = "rgba(239, 68, 68, 0.3)";
    $("sipStatusBadge").style.color = "#fca5a5";
    $("btnRegister").disabled = false;
    $("btnUnregister").disabled = true;
    $("btnSendMessage").disabled = true;
  }

  updateCallButtonState();
}

// Update call button state based on registration and session
function updateCallButtonState() {
  const hasSession = state.sessionId != null;
  const isRegistered = state.isRegistered;

  // Call button requires both session and registration
  $("btnCall").disabled = !(hasSession && isRegistered);
}

// Update SIP UI when WebSocket connects/disconnects
function updateSIPUI(connected) {
  if (connected) {
    $("btnRegister").disabled = false;
    $("btnUnregister").disabled = true;
  } else {
    $("btnRegister").disabled = true;
    $("btnUnregister").disabled = true;
    $("btnSendMessage").disabled = true;
    state.isRegistered = false;
    $("sipStatusBadge").textContent = "Not Registered";
    $("sipStatusBadge").style.background = "rgba(239, 68, 68, 0.3)";
    $("sipStatusBadge").style.color = "#fca5a5";
  }
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

  if (!state.isRegistered) {
    log("Must be registered to send messages", "error");
    return;
  }

  // Server will automatically route to the correct destination via session
  state.ws.send(
    JSON.stringify({
      type: "send_message",
      destination: "", // Server handles routing via active session
      body: body,
      contentType: "text/plain;charset=UTF-8",
    })
  );

  // Show sent message in UI
  const container = $("messageContainer");
  const div = document.createElement("div");
  div.className = "log-entry info";
  const time = new Date().toLocaleTimeString("en-US", { hour12: false });
  div.innerHTML = `<span class="time">${time}</span><b>You -> ${dest}:</b> ${body}`;
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

  log("Ready to connect.");
};
