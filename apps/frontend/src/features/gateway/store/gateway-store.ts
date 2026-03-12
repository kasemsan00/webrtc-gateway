import { Store } from '@tanstack/store'

import {
  defaultVideoConfig,
  gatewayRuntimeConfig,
  normalizeGatewayWssUrl,
} from '../config'
import { fetchVrsCredentials } from '../services/vrs-api'
import {
  applyIncomingRtt,
  buildOutgoingRttXml,
  isRttXmlPayload,
} from '../services/rtt-xep0301'
import {
  applyH264CodecPreference,
  applyVideoConstraints,
  buildIceServers,
  formatRtcStats,
  waitForIceGatheringComplete,
} from '../services/webrtc-client'
import {
  createGatewayWebSocket,
  isWebSocketOpen,
  sendJson,
} from '../services/ws-client'
import { sendSwitchRequest } from '../services/switch-api'
import type {
  CallMode,
  CallStatus,
  GatewayState,
  LogEntry,
  LogType,
  MediaInputDeviceOption,
  MessageEntry,
  PendingCallRequest,
  PublicCredentials,
  TrunkCredentials,
  TrunkStatus,
  VrsConfig,
  VrsFetchStatus,
} from '../types'
import { attachPersist } from '@/lib/store-persist'

// WebRTC types not available in global scope
type RTCRemoteInboundRtpStreamStats = RTCStats & {
  type: 'remote-inbound-rtp'
  kind: 'audio' | 'video'
  localId: string
  roundTripTime?: number
  totalRoundTripTime?: number
  fractionLost?: number
  roundTripTimeMeasurements?: number
}

type RTCInboundVideoStreamStats = RTCStats & {
  type: 'inbound-rtp'
  kind?: 'audio' | 'video'
  packetsLost?: number
  packetsReceived?: number
  frameWidth?: number
  frameHeight?: number
  bytesReceived?: number
  codecId?: string
}

const isBrowser = () => typeof window !== 'undefined'

const MAX_LOGS = 400
const MAX_MESSAGES = 300
const STATS_POLL_INTERVAL_MS = 1000
const WS_RECONNECT_MIN_DELAY_MS = 1000
const WS_RECONNECT_MAX_DELAY_MS = 15000
const RTT_SEND_THROTTLE_MS = 100

const initialState: GatewayState = {
  config: {
    wssUrl: gatewayRuntimeConfig.wssUrl,
  },
  connection: {
    status: 'disconnected',
    wsStateText: 'Disconnected',
  },
  media: {
    status: 'not-ready',
    rtcStateText: 'Not Ready',
    localStream: null,
    remoteVideoStream: null,
    remoteAudioStream: null,
    iceState: 'new',
    signalingState: 'stable',
  },
  call: {
    sessionId: null,
    state: 'idle',
    elapsedSeconds: 0,
    callCount: 0,
  },
  mode: 'siptrunk',
  publicCredentials: {
    sipDomain: '',
    sipUsername: '',
    sipPassword: '',
    sipPort: 5060,
  },
  vrs: {
    config: {
      phone: '',
      fullName: '',
      agency: 'spinsoft',
      emergency: 0,
      emergencyOptionsData: null,
      userAgent: 'web',
      mobileUID: '',
    },
    fetchStatus: 'idle',
    resolvedCredentials: null,
  },
  trunk: {
    status: 'not-resolved',
    credentials: {
      trunkId: '',
      sipDomain: '',
      sipUsername: '',
      sipPassword: '',
      sipPort: 5060,
    },
  },
  controls: {
    destination: '9999',
    dialpadOpen: false,
    statsOpen: false,
    isMutedAudio: false,
    isMutedVideo: false,
    autoStartingSession: false,
    selectedVideoInputId: '',
    selectedAudioInputId: '',
    availableVideoInputs: [],
    availableAudioInputs: [],
    mediaInputsLoading: false,
    switchingVideoInput: false,
    switchingAudioInput: false,
  },
  stats: {
    rttMs: '-',
    packetLossPercent: '-',
    bitrateKbps: '-',
    codec: '-',
    resolution: '-',
  },
  rtt: {
    remotePreviewText: '',
    remoteActive: false,
    lastRemoteSeq: null,
  },
  incomingCall: null,
  incomingAction: 'idle',
  logs: [],
  messages: [],
  pendingCallRequest: null,
}

export const gatewayStore = new Store<GatewayState>(initialState)

type TrunkResolvePayload = {
  trunkId?: number
  trunkPublicId?: string
  sipDomain?: string
  sipUsername?: string
  sipPassword?: string
  sipPort?: number
}

const runtime = {
  initialized: false,
  ws: null as WebSocket | null,
  pc: null as RTCPeerConnection | null,
  localStream: null as MediaStream | null,
  remoteStream: null as MediaStream | null,
  activeCallSessionId: null as string | null,
  pingInterval: null as ReturnType<typeof setInterval> | null,
  statsTimeout: null as ReturnType<typeof setTimeout> | null,
  statsInFlight: false,
  statsCycle: 0,
  callTimerInterval: null as ReturnType<typeof setInterval> | null,
  statsSnapshot: null as { bytes: number; at: number } | null,
  pendingRedirectUrl: null as string | null,
  reconnectTimeout: null as ReturnType<typeof setTimeout> | null,
  reconnectAttempts: 0,
  manualDisconnect: false,
  reconnectTargetUrl: null as string | null,
  resumePending: false,
  resumeSessionId: null as string | null,
  resumeRetryToken: 0,
  resumeRedirectUrl: null as string | null,
  wasCallActiveOnDisconnect: false,
  trunkResolvePayload: null as TrunkResolvePayload | null,
  trunkResolvePending: false,
  lastTrunkNotFoundAt: 0,
  pendingIncomingAcceptSessionId: null as string | null,
  videoConfig: { ...defaultVideoConfig },
  unsubscribePersist: null as (() => void) | null,
  outgoingRttSeq: 1,
  outgoingRttActive: false,
  outgoingRttLastSentText: '',
  outgoingRttTimer: null as ReturnType<typeof setTimeout> | null,
  outgoingRttPendingText: '',
  mediaDeviceChangeHandler: null as (() => void) | null,
}

function randomId() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function nowText() {
  return new Date().toLocaleTimeString('th-TH', {
    hour12: false,
    timeZone: 'Asia/Bangkok',
  })
}

function appendLog(message: string, type: LogType = 'info') {
  const entry: LogEntry = {
    id: randomId(),
    message,
    time: nowText(),
    type,
  }
  gatewayStore.setState((state) => ({
    ...state,
    logs: [...state.logs, entry].slice(-MAX_LOGS),
  }))
}

function appendMessage(
  from: string,
  body: string,
  direction: 'incoming' | 'outgoing',
) {
  const entry: MessageEntry = {
    id: randomId(),
    from,
    body,
    direction,
    time: nowText(),
  }
  gatewayStore.setState((state) => ({
    ...state,
    messages: [...state.messages, entry].slice(-MAX_MESSAGES),
  }))
}

function resetOutgoingRttRuntime() {
  if (runtime.outgoingRttTimer) {
    clearTimeout(runtime.outgoingRttTimer)
    runtime.outgoingRttTimer = null
  }
  runtime.outgoingRttSeq = 1
  runtime.outgoingRttActive = false
  runtime.outgoingRttLastSentText = ''
  runtime.outgoingRttPendingText = ''
}

function clearRemoteRttState() {
  gatewayStore.setState((state) => ({
    ...state,
    rtt: {
      ...state.rtt,
      remotePreviewText: '',
      remoteActive: false,
      lastRemoteSeq: null,
    },
  }))
}

function sendOutgoingRttPacket(
  text: string,
  event: 'new' | 'reset',
  { resetAfterSend = false }: { resetAfterSend?: boolean } = {},
) {
  if (!isWebSocketOpen(runtime.ws)) {
    if (resetAfterSend) resetOutgoingRttRuntime()
    return false
  }

  const xml = buildOutgoingRttXml(text, runtime.outgoingRttSeq, event)
  runtime.outgoingRttSeq += 1

  const sent = sendJson(runtime.ws, {
    type: 'send_message',
    destination: '',
    body: xml,
    contentType: 'text/plain;charset=UTF-8',
  })

  if (!sent) {
    if (resetAfterSend) resetOutgoingRttRuntime()
    return false
  }

  runtime.outgoingRttActive = text.length > 0
  runtime.outgoingRttLastSentText = text
  if (resetAfterSend) resetOutgoingRttRuntime()
  return true
}

function flushOutgoingRttDraft() {
  runtime.outgoingRttTimer = null

  const nextText = runtime.outgoingRttPendingText
  const previousText = runtime.outgoingRttLastSentText
  const isActive = runtime.outgoingRttActive

  if (nextText === previousText) return
  if (!nextText && !isActive) return

  const event: 'new' | 'reset' = isActive ? 'reset' : 'new'
  void sendOutgoingRttPacket(nextText, event)
}

/** Shape of the data we persist to localStorage. */
interface PersistedGatewayPrefs {
  mode: CallMode
  destination: string
  publicCredentials: PublicCredentials
  trunkCredentials: TrunkCredentials
  vrsConfig?: VrsConfig
  selectedVideoInputId?: string
  selectedAudioInputId?: string
}

const PERSIST_KEY = 'k2_gateway_prefs'
const PERSIST_VERSION = 1

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

function parseTrunkIdentifier(
  raw: string,
):
  | { kind: 'numeric'; trunkId: number }
  | { kind: 'public'; trunkPublicId: string }
  | null {
  const value = raw.trim()
  if (!value) return null

  const trunkId = Number.parseInt(value, 10)
  if (Number.isInteger(trunkId) && trunkId > 0 && String(trunkId) === value) {
    return { kind: 'numeric', trunkId }
  }

  if (UUID_PATTERN.test(value)) {
    return { kind: 'public', trunkPublicId: value }
  }

  return null
}

function buildTrunkResolvePayloadFromState(): TrunkResolvePayload | null {
  const state = gatewayStore.state
  if (state.mode !== 'siptrunk') return null

  const creds = state.trunk.credentials
  const parsedTrunk = parseTrunkIdentifier(creds.trunkId)
  if (!parsedTrunk) return null

  return parsedTrunk.kind === 'numeric'
    ? { trunkId: parsedTrunk.trunkId }
    : { trunkPublicId: parsedTrunk.trunkPublicId }
}

function selectedDeviceConstraint(deviceId: string) {
  const trimmed = deviceId.trim()
  if (!trimmed) return undefined
  return { exact: trimmed }
}

function buildVideoConstraint(deviceId: string): MediaTrackConstraints {
  const constraint: MediaTrackConstraints = {
    width: { ideal: runtime.videoConfig.width },
    height: { ideal: runtime.videoConfig.height },
    frameRate: { max: runtime.videoConfig.maxFramerate },
  }
  const selected = selectedDeviceConstraint(deviceId)
  if (selected) {
    constraint.deviceId = selected
  }
  return constraint
}

function buildPreferredGetUserMediaConstraints() {
  const controls = gatewayStore.state.controls
  const selectedAudio = selectedDeviceConstraint(controls.selectedAudioInputId)

  return {
    audio: selectedAudio ? { deviceId: selectedAudio } : true,
    video: buildVideoConstraint(controls.selectedVideoInputId),
  }
}

function mapDeviceOptions(
  devices: Array<MediaDeviceInfo>,
  kind: 'audioinput' | 'videoinput',
  fallbackLabelPrefix: string,
): Array<MediaInputDeviceOption> {
  const filtered = devices.filter(
    (device) => device.kind === kind && device.deviceId.trim().length > 0,
  )
  return filtered.map((device, index) => ({
    deviceId: device.deviceId,
    label: device.label.trim() || `${fallbackLabelPrefix} ${index + 1}`,
  }))
}

function fallbackToAvailableInput(
  options: Array<MediaInputDeviceOption>,
  selectedId: string,
) {
  if (!selectedId) return selectedId
  const exists = options.some((option) => option.deviceId === selectedId)
  return exists ? selectedId : ''
}

function updateLocalStreamTrack(
  kind: 'audio' | 'video',
  newTrack: MediaStreamTrack,
) {
  const current = runtime.localStream
  if (!current) {
    runtime.localStream = new MediaStream([newTrack])
    return
  }

  const keep = current.getTracks().filter((track) => track.kind !== kind)
  const previous = current.getTracks().filter((track) => track.kind === kind)
  const next = new MediaStream([...keep, newTrack])

  previous.forEach((track) => track.stop())
  runtime.localStream = next
}

export function hasCredentialReady(state: GatewayState) {
  if (state.mode === 'siptrunk') {
    return parseTrunkIdentifier(state.trunk.credentials.trunkId) !== null
  }

  if (state.mode === 'publicvrs') {
    return Boolean(
      state.vrs.config.phone.trim() && state.vrs.config.fullName.trim(),
    )
  }

  return Boolean(
    state.publicCredentials.sipDomain.trim() &&
    state.publicCredentials.sipUsername.trim() &&
    state.publicCredentials.sipPassword,
  )
}

export function isCallInProgress(state: GatewayState) {
  return (
    state.call.state === 'connecting' ||
    state.call.state === 'ringing' ||
    state.call.state === 'reconnecting' ||
    state.call.state === 'active'
  )
}

export function canPlaceCall(state: GatewayState) {
  return (
    state.connection.status === 'connected' &&
    hasCredentialReady(state) &&
    !isCallInProgress(state) &&
    !state.controls.autoStartingSession
  )
}

export function canResolveTrunk(state: GatewayState) {
  const hasTrunkId = parseTrunkIdentifier(state.trunk.credentials.trunkId) !== null
  return state.connection.status === 'connected' && hasTrunkId
}

export function normalizeCallStatus(value: string): CallStatus {
  if (
    value === 'connecting' ||
    value === 'ringing' ||
    value === 'reconnecting' ||
    value === 'active' ||
    value === 'ended'
  ) {
    return value
  }
  return 'idle'
}

function startPingInterval() {
  stopPingInterval()
  runtime.pingInterval = setInterval(() => {
    if (!isWebSocketOpen(runtime.ws)) return
    sendJson(runtime.ws, { type: 'ping' })
  }, 20_000)
}

function stopPingInterval() {
  if (!runtime.pingInterval) return
  clearInterval(runtime.pingInterval)
  runtime.pingInterval = null
}

function clearReconnectTimeout() {
  if (!runtime.reconnectTimeout) return
  clearTimeout(runtime.reconnectTimeout)
  runtime.reconnectTimeout = null
}

function clearResumeRecovery() {
  runtime.resumePending = false
  runtime.resumeSessionId = null
  runtime.resumeRedirectUrl = null
  runtime.resumeRetryToken = 0
  runtime.wasCallActiveOnDisconnect = false
}

function scheduleReconnect() {
  if (runtime.manualDisconnect || runtime.reconnectTimeout) return

  const attempt = runtime.reconnectAttempts
  const baseDelay = Math.min(
    WS_RECONNECT_MAX_DELAY_MS,
    WS_RECONNECT_MIN_DELAY_MS * Math.pow(2, attempt),
  )
  const jitter = 0.85 + Math.random() * 0.3
  const delayMs = Math.round(baseDelay * jitter)
  runtime.reconnectAttempts += 1

  appendLog(`WebSocket reconnect in ${(delayMs / 1000).toFixed(1)}s`, 'warning')

  runtime.reconnectTimeout = setTimeout(() => {
    runtime.reconnectTimeout = null
    if (runtime.manualDisconnect) return
    connect(runtime.reconnectTargetUrl ?? undefined)
  }, delayMs)
}

function startTimer() {
  stopTimer()
  const startedAt = Date.now()
  runtime.callTimerInterval = setInterval(() => {
    const elapsed = Math.floor((Date.now() - startedAt) / 1000)
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        elapsedSeconds: elapsed,
      },
    }))
  }, 1000)
}

function stopTimer({ resetElapsed = true }: { resetElapsed?: boolean } = {}) {
  if (runtime.callTimerInterval) {
    clearInterval(runtime.callTimerInterval)
    runtime.callTimerInterval = null
  }
  if (!resetElapsed) return
  gatewayStore.setState((state) => ({
    ...state,
    call: {
      ...state.call,
      elapsedSeconds: 0,
    },
  }))
}

function startStats() {
  if (runtime.statsTimeout || runtime.statsInFlight) return

  runtime.statsCycle += 1
  const cycle = runtime.statsCycle

  const run = async () => {
    if (cycle !== runtime.statsCycle) return
    if (!gatewayStore.state.controls.statsOpen) return
    if (!runtime.pc) {
      runtime.statsTimeout = setTimeout(run, STATS_POLL_INTERVAL_MS)
      return
    }
    if (runtime.statsInFlight) {
      runtime.statsTimeout = setTimeout(run, STATS_POLL_INTERVAL_MS)
      return
    }

    runtime.statsInFlight = true
    try {
      const reports = await runtime.pc.getStats()
      if (cycle !== runtime.statsCycle) {
        return
      }

      let roundTripTime: number | undefined
      let inboundVideo: RTCInboundVideoStreamStats | undefined

      reports.forEach((report) => {
        if (
          report.type === 'remote-inbound-rtp' &&
          (report as RTCRemoteInboundRtpStreamStats).kind === 'video'
        ) {
          roundTripTime = (report as RTCRemoteInboundRtpStreamStats)
            .roundTripTime
          return
        }

        if (
          report.type === 'inbound-rtp' &&
          (report as RTCInboundVideoStreamStats).kind === 'video'
        ) {
          inboundVideo = report as RTCInboundVideoStreamStats
        }
      })

      const video = inboundVideo
      if (!video) {
        return
      }

      const base = formatRtcStats({
        roundTripTime,
        packetsLost: video.packetsLost,
        packetsReceived: video.packetsReceived,
        frameWidth: video.frameWidth,
        frameHeight: video.frameHeight,
        bytesReceived: video.bytesReceived,
      })

      let bitrateKbps = '-'
      if (typeof video.bytesReceived === 'number') {
        const now = Date.now()
        if (runtime.statsSnapshot) {
          const bytesDelta = video.bytesReceived - runtime.statsSnapshot.bytes
          const timeDelta = (now - runtime.statsSnapshot.at) / 1000
          if (timeDelta > 0) {
            bitrateKbps = `${Math.max(0, Math.round((bytesDelta * 8) / 1000 / timeDelta))}`
          }
        }
        runtime.statsSnapshot = { bytes: video.bytesReceived, at: now }
      }

      gatewayStore.setState((state) => ({
        ...state,
        stats: {
          ...state.stats,
          ...base,
          bitrateKbps,
          codec: video.codecId ?? '-',
        },
      }))
    } catch (error) {
      appendLog(
        `Could not collect stats: ${(error as Error).message}`,
        'warning',
      )
    } finally {
      runtime.statsInFlight = false
      if (cycle === runtime.statsCycle) {
        runtime.statsTimeout = setTimeout(run, STATS_POLL_INTERVAL_MS)
      }
    }
  }

  void run()
}

function stopStats() {
  runtime.statsCycle += 1
  runtime.statsInFlight = false
  if (runtime.statsTimeout) {
    clearTimeout(runtime.statsTimeout)
    runtime.statsTimeout = null
  }
  runtime.statsSnapshot = null
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      statsOpen: false,
    },
    stats: {
      rttMs: '-',
      packetLossPercent: '-',
      bitrateKbps: '-',
      codec: '-',
      resolution: '-',
    },
  }))
}

function teardownFullSession({ preserveCallState = false } = {}) {
  stopTimer()
  stopStats()
  resetOutgoingRttRuntime()
  runtime.pendingIncomingAcceptSessionId = null

  if (runtime.localStream) {
    runtime.localStream.getTracks().forEach((track) => track.stop())
    runtime.localStream = null
  }

  if (runtime.pc) {
    runtime.pc.close()
    runtime.pc = null
  }

  runtime.activeCallSessionId = null
  runtime.remoteStream = null

  gatewayStore.setState((state) => ({
    ...state,
    media: {
      status: 'not-ready',
      rtcStateText: 'Not Ready',
      localStream: null,
      remoteVideoStream: null,
      remoteAudioStream: null,
      iceState: 'new',
      signalingState: 'stable',
    },
    call: {
      ...state.call,
      sessionId: null,
      state: preserveCallState ? state.call.state : 'idle',
    },
    controls: {
      ...state.controls,
      isMutedAudio: false,
      isMutedVideo: false,
      autoStartingSession: false,
      statsOpen: false,
      switchingVideoInput: false,
      switchingAudioInput: false,
    },
    rtt: {
      ...state.rtt,
      remotePreviewText: '',
      remoteActive: false,
      lastRemoteSeq: null,
    },
    incomingCall: null,
    incomingAction: 'idle',
    pendingCallRequest: null,
  }))
}

function teardownSessionForRecovery() {
  stopStats()
  resetOutgoingRttRuntime()
  stopTimer({ resetElapsed: false })

  if (runtime.pc) {
    runtime.pc.close()
    runtime.pc = null
  }

  runtime.remoteStream = null

  gatewayStore.setState((state) => ({
    ...state,
    media: {
      ...state.media,
      status: 'not-ready',
      rtcStateText: 'Reconnecting...',
      remoteVideoStream: null,
      remoteAudioStream: null,
      iceState: 'new',
      signalingState: 'stable',
    },
    controls: {
      ...state.controls,
      autoStartingSession: false,
      statsOpen: false,
      switchingVideoInput: false,
      switchingAudioInput: false,
    },
    incomingCall: null,
    incomingAction: 'idle',
  }))
}

function handleCallState(callState: string) {
  const normalized = normalizeCallStatus(callState)
  gatewayStore.setState((state) => ({
    ...state,
    call: {
      ...state.call,
      state: normalized,
    },
    incomingAction:
      normalized === 'active' || normalized === 'ended'
        ? 'idle'
        : state.incomingAction,
  }))
  appendLog(`Call State: ${normalized}`, 'info')

  if (normalized === 'active') {
    runtime.activeCallSessionId = gatewayStore.state.call.sessionId
    startTimer()
    return
  }

  if (normalized === 'connecting' || normalized === 'ringing') {
    runtime.activeCallSessionId = gatewayStore.state.call.sessionId
    return
  }

  if (normalized === 'reconnecting') {
    runtime.activeCallSessionId = gatewayStore.state.call.sessionId
    return
  }

  if (normalized === 'ended') {
    runtime.activeCallSessionId = null
    clearResumeRecovery()
    teardownFullSession()
  }
}

function flushPendingCallQueue() {
  const pending = gatewayStore.state.pendingCallRequest
  const sessionId = gatewayStore.state.call.sessionId
  if (!pending || !sessionId) return

  appendLog('Auto-placing queued call...', 'info')
  gatewayStore.setState((state) => ({
    ...state,
    pendingCallRequest: null,
  }))
  sendCallPayload(pending)
}

function flushPendingIncomingAcceptQueue() {
  const incomingSessionId = runtime.pendingIncomingAcceptSessionId
  if (!incomingSessionId) return
  if (!isWebSocketOpen(runtime.ws)) return

  const sent = sendJson(runtime.ws, {
    type: 'accept',
    sessionId: incomingSessionId,
  })
  if (!sent) {
    gatewayStore.setState((state) => ({
      ...state,
      incomingAction: 'idle',
    }))
    appendLog('Unable to send accept: WebSocket not connected', 'warning')
    return
  }
  runtime.pendingIncomingAcceptSessionId = null
  gatewayStore.setState((state) => ({
    ...state,
    incomingCall: null,
    incomingAction: 'sending_accept',
  }))
  appendLog('Incoming call accepted after media session became ready', 'success')
}

function handlePublicIdentityChangedError() {
  const pending = buildCallParams()
  if (!pending) {
    appendLog(
      'Public SIP identity changed. Start a new media session before calling again.',
      'warning',
    )
    return
  }

  appendLog(
    'Public SIP identity changed. Rebuilding media session for new identity...',
    'warning',
  )

  clearResumeRecovery()
  teardownFullSession()
  gatewayStore.setState((state) => ({
    ...state,
    pendingCallRequest: pending,
  }))
  ensureMediaSessionForCall()
}

function handleIncomingCall(payload: {
  from?: string
  to?: string
  sessionId?: string
}) {
  const sessionId = payload.sessionId || gatewayStore.state.call.sessionId
  if (!sessionId) {
    appendLog('Incoming call ignored: missing sessionId', 'warning')
    return
  }

  gatewayStore.setState((state) => ({
    ...state,
    call: {
      ...state.call,
      sessionId,
    },
    incomingCall: {
      from: payload.from ?? 'Unknown',
      to: payload.to ?? 'Unknown',
      mode: state.mode,
      sessionId,
    },
    incomingAction: 'idle',
  }))

  appendLog(
    `Incoming call from ${payload.from ?? 'Unknown'} (to: ${payload.to ?? 'Unknown'})`,
    'info',
  )
}

async function handleAnswer(payload: { sdp: string; sessionId: string }) {
  if (!runtime.pc) return

  try {
    await runtime.pc.setRemoteDescription({
      type: 'answer',
      sdp: payload.sdp,
    })

    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: payload.sessionId,
      },
      media: {
        ...state.media,
        status: 'active',
        rtcStateText: 'Active',
      },
      controls: {
        ...state.controls,
        autoStartingSession: false,
      },
    }))

    if (runtime.localStream) {
      await applyVideoConstraints(
        runtime.pc,
        runtime.localStream,
        runtime.videoConfig,
      )
    }
    appendLog('Session Established', 'success')
    flushPendingCallQueue()
    flushPendingIncomingAcceptQueue()
  } catch (error) {
    appendLog(
      `Error setting remote description: ${(error as Error).message}`,
      'error',
    )
    gatewayStore.setState((state) => ({
      ...state,
      controls: {
        ...state.controls,
        autoStartingSession: false,
      },
    }))
  }
}

function bindPeerConnectionEvents(pc: RTCPeerConnection) {
  pc.oniceconnectionstatechange = () => {
    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        iceState: runtime.pc?.iceConnectionState ?? 'new',
      },
    }))
    appendLog(`ICE State: ${runtime.pc?.iceConnectionState ?? 'new'}`, 'info')
  }

  pc.onsignalingstatechange = () => {
    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        signalingState: runtime.pc?.signalingState ?? 'stable',
      },
    }))
  }

  pc.ontrack = (event) => {
    if (!runtime.remoteStream) {
      runtime.remoteStream = new MediaStream()
    }
    const remoteStream = runtime.remoteStream

    const addTrackIfMissing = (track: MediaStreamTrack) => {
      const exists = remoteStream
        .getTracks()
        .some((existing) => existing.id === track.id)
      if (!exists) {
        remoteStream.addTrack(track)
      }
    }

    event.streams[0]?.getTracks().forEach(addTrackIfMissing)
    addTrackIfMissing(event.track)

    const hasVideo = remoteStream.getVideoTracks().length > 0
    const hasAudio = remoteStream.getAudioTracks().length > 0

    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        remoteVideoStream: hasVideo
          ? remoteStream
          : state.media.remoteVideoStream,
        remoteAudioStream: hasAudio
          ? remoteStream
          : state.media.remoteAudioStream,
      },
    }))

    appendLog(
      `Remote Track: ${event.track.kind} (streams: ${event.streams.length}, video: ${hasVideo ? 'yes' : 'no'}, audio: ${hasAudio ? 'yes' : 'no'})`,
      'success',
    )
  }
}

function buildPeerConnectionFromCurrentLocalStream() {
  if (!runtime.localStream) {
    throw new Error('Local stream is not available')
  }

  runtime.pc = new RTCPeerConnection({
    iceServers: buildIceServers(gatewayRuntimeConfig.turn),
  })
  bindPeerConnectionEvents(runtime.pc)

  runtime.localStream
    .getTracks()
    .forEach((track) => runtime.pc?.addTrack(track, runtime.localStream!))

  applyH264CodecPreference(
    runtime.pc,
    runtime.videoConfig.useConstrainedBaseline,
  )
}

async function requestLocalMediaForResume() {
  if (runtime.localStream) {
    const hasLiveTrack = runtime.localStream
      .getTracks()
      .some((track) => track.readyState === 'live')
    if (hasLiveTrack) return
  }

  appendLog(
    'Local media unavailable during resume, requesting access...',
    'warning',
  )
  runtime.localStream = await navigator.mediaDevices.getUserMedia(
    buildPreferredGetUserMediaConstraints(),
  )

  gatewayStore.setState((state) => ({
    ...state,
    media: {
      ...state.media,
      localStream: runtime.localStream,
    },
  }))
  void refreshMediaInputDevices()
}

async function sendResumeOffer(sessionId: string) {
  runtime.resumeRetryToken += 1
  appendLog(
    `Resuming call session ${sessionId} (attempt ${runtime.resumeRetryToken})...`,
    'info',
  )

  try {
    await requestLocalMediaForResume()

    if (runtime.pc) {
      runtime.pc.close()
      runtime.pc = null
    }
    runtime.remoteStream = null

    buildPeerConnectionFromCurrentLocalStream()
    const pc = runtime.pc as RTCPeerConnection

    const offer = await pc.createOffer({
      offerToReceiveAudio: true,
      offerToReceiveVideo: true,
    })
    await pc.setLocalDescription(offer)
    await waitForIceGatheringComplete(pc)

    const sent = sendJson(runtime.ws, {
      type: 'resume',
      sessionId,
      sdp: pc.localDescription?.sdp,
    })
    if (!sent) {
      throw new Error('WebSocket is not connected')
    }

    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        status: 'offer-sent',
        rtcStateText: 'Resume Offer Sent...',
      },
    }))
    appendLog('Resume request sent', 'info')
    return true
  } catch (error) {
    appendLog(`Resume request failed: ${(error as Error).message}`, 'error')
    return false
  }
}

function sendRequestKeyframe(sessionId: string, source: string) {
  const sent = sendJson(runtime.ws, {
    type: 'request_keyframe',
    sessionId,
  })
  if (sent) {
    appendLog(`Requested keyframe (${source})`, 'info')
  }
}

async function handleResumed(payload: {
  sessionId?: string
  sdp?: string
  state?: string
}) {
  const resumedSessionId =
    payload.sessionId ||
    runtime.resumeSessionId ||
    gatewayStore.state.call.sessionId

  if (resumedSessionId) {
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: resumedSessionId,
      },
      media: {
        ...state.media,
        status: 'active',
        rtcStateText: 'Active',
      },
    }))
    runtime.activeCallSessionId = resumedSessionId
  }

  if (payload.sdp && runtime.pc) {
    try {
      await runtime.pc.setRemoteDescription({
        type: 'answer',
        sdp: payload.sdp,
      })
    } catch (error) {
      appendLog(
        `Resume answer apply failed: ${(error as Error).message}`,
        'error',
      )
    }
  }

  clearResumeRecovery()
  const resumedState =
    payload.state && payload.state !== 'reconnecting' ? payload.state : 'active'
  handleCallState(resumedState)
  if (resumedSessionId) {
    sendRequestKeyframe(resumedSessionId, 'resume')
  }
  appendLog(
    `Call resumed${resumedSessionId ? `: ${resumedSessionId}` : ''}`,
    'success',
  )
}

function handleResumeFailed(payload: { reason?: string }) {
  const reason = payload.reason || 'Unknown reason'
  appendLog(`Resume failed: ${reason}`, 'error')
  clearResumeRecovery()
  handleCallState('ended')
}

function handleResumeRedirect(payload: {
  sessionId?: string
  redirectUrl?: string
}) {
  if (payload.sessionId) {
    runtime.resumeSessionId = payload.sessionId
  }
  if (!runtime.resumeSessionId) {
    runtime.resumeSessionId = gatewayStore.state.call.sessionId
  }
  runtime.resumePending = true

  if (!payload.redirectUrl) {
    appendLog('Resume redirect URL missing', 'error')
    return
  }

  runtime.resumeRedirectUrl = payload.redirectUrl
  appendLog(`Resume redirect to ${payload.redirectUrl}`, 'warning')

  if (runtime.ws) {
    runtime.ws.close()
  } else {
    connect(payload.redirectUrl)
  }
}

function handleTrunkResolved(payload: {
  trunkId?: string | number
  trunkPublicId?: string
}) {
  const trunkIdText = payload.trunkId ? String(payload.trunkId) : ''
  const trunkId = Number.parseInt(trunkIdText, 10)
  const trunkPublicId = payload.trunkPublicId
    ? String(payload.trunkPublicId)
    : ''
  const hasPublicId = Boolean(trunkPublicId && UUID_PATTERN.test(trunkPublicId))

  if ((!Number.isInteger(trunkId) || trunkId <= 0) && !hasPublicId) {
    runtime.trunkResolvePending = false
    runtime.trunkResolvePayload = null
    runtime.lastTrunkNotFoundAt = Date.now()
    setTrunkStatus(
      'not-found',
      'Trunk not found: Invalid trunkId/trunkPublicId in trunk_resolved response',
      'error',
    )
    return
  }

  runtime.trunkResolvePending = false
  runtime.trunkResolvePayload = null

  gatewayStore.setState((state) => ({
    ...state,
    trunk: {
      ...state.trunk,
      status: 'resolved',
      credentials: {
        ...state.trunk.credentials,
        trunkId:
          hasPublicId
            ? trunkPublicId
            : Number.isInteger(trunkId) && trunkId > 0
              ? String(trunkId)
              : '',
      },
    },
  }))

  appendLog(
    Number.isInteger(trunkId) && trunkId > 0
      ? `Trunk resolved: ID ${trunkId}`
      : `Trunk resolved: UUID ${trunkPublicId}`,
    'success',
  )
}

function handleTrunkRedirect(payload: { redirectUrl?: string }) {
  runtime.trunkResolvePending = true
  gatewayStore.setState((state) => ({
    ...state,
    trunk: {
      ...state.trunk,
      status: 'redirecting',
    },
  }))

  if (!payload.redirectUrl) {
    appendLog('Redirect URL missing', 'error')
    return
  }

  runtime.pendingRedirectUrl = payload.redirectUrl
  appendLog(`Redirecting to ${payload.redirectUrl}`, 'warning')
  if (runtime.ws) {
    runtime.ws.close()
  } else {
    connect(payload.redirectUrl)
  }
}

function setTrunkStatus(
  status: TrunkStatus,
  message?: string,
  logType: LogType = 'info',
) {
  gatewayStore.setState((state) => ({
    ...state,
    trunk: {
      ...state.trunk,
      status,
    },
  }))
  if (message) {
    appendLog(message, logType)
  }
}

function handleMessage(event: MessageEvent<string>) {
  let message: Record<string, unknown>
  try {
    message = JSON.parse(event.data) as Record<string, unknown>
  } catch {
    appendLog('Received malformed server message', 'warning')
    return
  }

  switch (message.type) {
    case 'answer':
      void handleAnswer({
        sdp: String(message.sdp ?? ''),
        sessionId: String(message.sessionId ?? ''),
      })
      break
    case 'pong':
      appendLog('Pong received', 'success')
      break
    case 'state': {
      const nextState = String(message.state ?? 'idle')
      const sessionId = message.sessionId ? String(message.sessionId) : null
      if (sessionId && nextState !== 'ended') {
        gatewayStore.setState((state) => ({
          ...state,
          call: {
            ...state.call,
            sessionId,
          },
        }))
      }
      if (nextState === 'ended') {
        clearResumeRecovery()
      }
      handleCallState(nextState)
      break
    }
    case 'incoming':
      handleIncomingCall({
        from: String(message.from ?? 'Unknown'),
        to: String(message.to ?? 'Unknown'),
        sessionId: message.sessionId ? String(message.sessionId) : undefined,
      })
      break
    case 'trunk_resolved':
      handleTrunkResolved({
        trunkId: message.trunkId ? String(message.trunkId) : undefined,
        trunkPublicId: message.trunkPublicId
          ? String(message.trunkPublicId)
          : undefined,
      })
      break
    case 'resume_redirect':
      handleResumeRedirect({
        sessionId: message.sessionId ? String(message.sessionId) : undefined,
        redirectUrl: message.redirectUrl
          ? String(message.redirectUrl)
          : undefined,
      })
      break
    case 'resumed':
      void handleResumed({
        sessionId: message.sessionId ? String(message.sessionId) : undefined,
        sdp: message.sdp ? String(message.sdp) : undefined,
        state: message.state ? String(message.state) : undefined,
      })
      break
    case 'resume_failed':
      handleResumeFailed({
        reason: message.reason ? String(message.reason) : undefined,
      })
      break
    case 'trunk_redirect':
      handleTrunkRedirect({
        redirectUrl: message.redirectUrl
          ? String(message.redirectUrl)
          : undefined,
      })
      break
    case 'trunk_not_found':
      runtime.trunkResolvePending = false
      runtime.trunkResolvePayload = null
      runtime.lastTrunkNotFoundAt = Date.now()
      gatewayStore.setState((state) => ({
        ...state,
        trunk: {
          ...state.trunk,
          credentials: {
            ...state.trunk.credentials,
            trunkId: '',
          },
        },
      }))
      setTrunkStatus(
        'not-found',
        `Trunk not found: ${String(message.reason ?? 'No match')}`,
        'error',
      )
      break
    case 'trunk_not_ready':
      runtime.trunkResolvePending = false
      setTrunkStatus(
        'not-ready',
        `Trunk not ready: ${String(message.reason ?? 'Owner not discoverable')}`,
        'warning',
      )
      break
    case 'message':
      {
        console.log('message', message)
        const from = String(message.from ?? 'Remote')
        const body = String(message.body ?? '')
        const contentType = String(message.contentType ?? '')

        if (isRttXmlPayload(body)) {
          const result = applyIncomingRtt(
            gatewayStore.state.rtt.remotePreviewText,
            body,
          )
          if (!result.parsed) {
            appendLog(
              `RTT parse failed for ${from}${contentType ? ` (${contentType})` : ''}`,
              'warning',
            )
            appendMessage(from, body, 'incoming')
            break
          }

          gatewayStore.setState((state) => ({
            ...state,
            rtt: {
              ...state.rtt,
              remotePreviewText: result.nextText,
              remoteActive: true,
              lastRemoteSeq: result.seq,
            },
          }))
          break
        }

        clearRemoteRttState()
        appendMessage(from, body, 'incoming')
      }
      break
    case 'messageSent':
      appendLog(
        `Message sent to ${String(message.destination ?? 'active peer')}`,
        'success',
      )
      break
    case 'dtmf':
      appendLog(`DTMF received: ${String(message.digits ?? '-')}`, 'info')
      break
    case 'error': {
      const error = String(message.error ?? 'Unknown server error')
      if (
        error.startsWith('Trunk not found:') &&
        Date.now() - runtime.lastTrunkNotFoundAt < 2000
      ) {
        break
      }
      appendLog(`Server Error: ${error}`, 'error')
      gatewayStore.setState((state) => ({
        ...state,
        incomingAction: 'idle',
      }))
      if (error.includes('Public SIP identity changed')) {
        handlePublicIdentityChangedError()
        break
      }
      if (error.includes('Session not found')) {
        handleCallState('ended')
      }
      break
    }
    default:
      break
  }
}

export function connect(urlOverride?: string) {
  if (!isBrowser()) return
  runtime.manualDisconnect = false
  clearReconnectTimeout()

  const targetUrl = normalizeGatewayWssUrl(
    urlOverride ?? gatewayStore.state.config.wssUrl,
  )
  runtime.reconnectTargetUrl = targetUrl
  const reconnecting =
    runtime.resumePending ||
    runtime.reconnectAttempts > 0 ||
    gatewayStore.state.connection.status === 'reconnecting'
  gatewayStore.setState((state) => ({
    ...state,
    connection: {
      status: reconnecting ? 'reconnecting' : 'connecting',
      wsStateText: reconnecting ? 'Reconnecting...' : 'Connecting...',
    },
  }))
  appendLog(
    `${reconnecting ? 'Reconnecting' : 'Connecting'} to ${targetUrl}...`,
    'info',
  )

  if (runtime.ws && runtime.ws.readyState <= WebSocket.OPEN) {
    runtime.ws.onclose = null
    runtime.ws.close()
  }

  try {
    const ws = createGatewayWebSocket(targetUrl)
    runtime.ws = ws

    ws.onopen = () => {
      runtime.reconnectAttempts = 0
      gatewayStore.setState((state) => ({
        ...state,
        connection: {
          status: 'connected',
          wsStateText: 'Connected',
        },
      }))
      appendLog('WebSocket Connected', 'success')
      startPingInterval()

      if (runtime.resumePending && runtime.resumeSessionId) {
        void sendResumeOffer(runtime.resumeSessionId).then((sent) => {
          if (!sent) {
            clearResumeRecovery()
            handleCallState('ended')
          }
        })
        return
      }

      if (
        !runtime.trunkResolvePending &&
        gatewayStore.state.mode === 'siptrunk'
      ) {
        const payload = buildTrunkResolvePayloadFromState()
        if (payload) {
          runtime.trunkResolvePayload = payload
          runtime.trunkResolvePending = true
        }
      }

      if (runtime.trunkResolvePending && runtime.trunkResolvePayload) {
        sendJson(runtime.ws, {
          type: 'trunk_resolve',
          ...runtime.trunkResolvePayload,
        })
      }
    }

    ws.onclose = () => {
      stopPingInterval()
      if (runtime.ws === ws) {
        runtime.ws = null
      }

      const sessionId = gatewayStore.state.call.sessionId
      const callState = gatewayStore.state.call.state
      const shouldRecoverCall =
        !runtime.manualDisconnect &&
        Boolean(sessionId) &&
        callState !== 'idle' &&
        callState !== 'ended'

      if (shouldRecoverCall && sessionId) {
        runtime.resumePending = true
        runtime.resumeSessionId = sessionId
        runtime.wasCallActiveOnDisconnect = true

        gatewayStore.setState((state) => ({
          ...state,
          connection: {
            status: 'reconnecting',
            wsStateText: 'Reconnecting...',
          },
          call: {
            ...state.call,
            state: 'reconnecting',
          },
        }))
        appendLog('WebSocket Disconnected - attempting call resume', 'warning')
        teardownSessionForRecovery()
      } else {
        clearResumeRecovery()
        gatewayStore.setState((state) => ({
          ...state,
          connection: {
            status: 'disconnected',
            wsStateText: 'Disconnected',
          },
        }))
        appendLog('WebSocket Disconnected', 'warning')
        teardownFullSession()
      }

      if (runtime.resumeRedirectUrl) {
        const redirect = runtime.resumeRedirectUrl
        runtime.resumeRedirectUrl = null
        setTimeout(() => connect(redirect), 200)
        return
      }

      if (runtime.pendingRedirectUrl) {
        const redirect = runtime.pendingRedirectUrl
        runtime.pendingRedirectUrl = null
        setTimeout(() => connect(redirect), 200)
        return
      }

      scheduleReconnect()
    }

    ws.onerror = () => {
      gatewayStore.setState((state) => ({
        ...state,
        connection: {
          status: 'error',
          wsStateText: 'Error',
        },
      }))
      appendLog('WebSocket Error', 'error')
    }

    ws.onmessage = handleMessage
  } catch (error) {
    gatewayStore.setState((state) => ({
      ...state,
      connection: {
        status: 'error',
        wsStateText: 'Error',
      },
    }))
    appendLog(`Connection failed: ${(error as Error).message}`, 'error')
  }
}

export function disconnect() {
  runtime.manualDisconnect = true
  clearReconnectTimeout()
  clearResumeRecovery()
  runtime.pendingRedirectUrl = null
  runtime.resumeRedirectUrl = null
  if (runtime.ws) {
    runtime.ws.close()
  }
}

export function toggleConnect() {
  if (runtime.ws && runtime.ws.readyState <= WebSocket.OPEN) {
    disconnect()
    return
  }
  connect()
}

export async function startSession() {
  if (!isBrowser()) return
  if (!isWebSocketOpen(runtime.ws)) {
    appendLog('WebSocket not connected', 'error')
    return
  }

  if (runtime.pc) {
    endSession()
    return
  }

  try {
    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        status: 'getting-media',
        rtcStateText: 'Getting User Media...',
      },
    }))
    appendLog('Requesting Media Access...', 'info')

    runtime.localStream = await navigator.mediaDevices.getUserMedia(
      buildPreferredGetUserMediaConstraints(),
    )

    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        localStream: runtime.localStream,
      },
    }))
    void refreshMediaInputDevices()
    appendLog('Media Access Granted', 'success')

    buildPeerConnectionFromCurrentLocalStream()

    const pc = runtime.pc as RTCPeerConnection

    const offer = await pc.createOffer({
      offerToReceiveAudio: true,
      offerToReceiveVideo: true,
    })
    await pc.setLocalDescription(offer)
    await waitForIceGatheringComplete(pc)

    const sent = sendJson(runtime.ws, {
      type: 'offer',
      sdp: pc.localDescription?.sdp,
    })
    if (!sent) {
      throw new Error('WebSocket is not connected')
    }

    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        status: 'offer-sent',
        rtcStateText: 'Offer Sent...',
      },
    }))
  } catch (error) {
    appendLog(`Session Start Failed: ${(error as Error).message}`, 'error')
    teardownFullSession()
  }
}

export function endSession() {
  clearRemoteRttState()
  resetOutgoingRttRuntime()
  hangup()
  teardownFullSession()
}

function sendCallPayload(params: PendingCallRequest) {
  const sessionId = gatewayStore.state.call.sessionId
  if (!sessionId) {
    appendLog('Cannot place call without active session', 'error')
    return
  }

  const payload: Record<string, unknown> = {
    type: 'call',
    sessionId,
    destination: params.destination,
  }

  if (params.trunkPublicId) {
    payload.trunkPublicId = params.trunkPublicId
  } else if (params.trunkId) {
    payload.trunkId = params.trunkId
  } else {
    payload.sipDomain = params.sipDomain
    payload.sipUsername = params.sipUsername
    payload.sipPassword = params.sipPassword
    payload.sipPort = params.sipPort
  }

  if (!sendJson(runtime.ws, payload)) {
    appendLog('WebSocket not connected', 'error')
    return
  }

  gatewayStore.setState((state) => ({
    ...state,
    call: {
      ...state.call,
      callCount: state.call.callCount + 1,
    },
  }))
  appendLog(
    `Calling ${params.destination}... (Call #${gatewayStore.state.call.callCount}, mode: ${gatewayStore.state.mode})`,
    'info',
  )
  // destination is auto-persisted via store subscription
}

function ensureMediaSessionForCall() {
  if (gatewayStore.state.controls.autoStartingSession || runtime.pc) return
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      autoStartingSession: true,
    },
  }))

  void startSession().finally(() => {
    gatewayStore.setState((state) => ({
      ...state,
      controls: {
        ...state.controls,
        autoStartingSession: false,
      },
    }))
  })
}

function buildCallParams(): PendingCallRequest | null {
  const destination = gatewayStore.state.controls.destination.trim()
  if (!destination) {
    appendLog('Please enter a destination', 'error')
    return null
  }

  const params: PendingCallRequest = { destination }

  if (gatewayStore.state.mode === 'siptrunk') {
    const parsedTrunk = parseTrunkIdentifier(
      gatewayStore.state.trunk.credentials.trunkId,
    )
    if (!parsedTrunk) {
      appendLog('Trunk ID required in SIP Trunk mode (number or UUID)', 'error')
      return null
    }
    if (parsedTrunk.kind === 'numeric') {
      params.trunkId = parsedTrunk.trunkId
    } else {
      params.trunkPublicId = parsedTrunk.trunkPublicId
    }
  } else if (gatewayStore.state.mode === 'publicvrs') {
    const resolved = gatewayStore.state.vrs.resolvedCredentials
    if (!resolved) {
      appendLog('VRS credentials not yet resolved', 'error')
      return null
    }
    params.sipDomain = resolved.sipDomain
    params.sipUsername = resolved.sipUsername
    params.sipPassword = resolved.sipPassword
    params.sipPort = resolved.sipPort
  } else {
    const creds = gatewayStore.state.publicCredentials
    if (
      !creds.sipDomain.trim() ||
      !creds.sipUsername.trim() ||
      !creds.sipPassword
    ) {
      appendLog('SIP credentials required in Public mode', 'error')
      return null
    }
    params.sipDomain = creds.sipDomain.trim()
    params.sipUsername = creds.sipUsername.trim()
    params.sipPassword = creds.sipPassword
    params.sipPort = creds.sipPort
  }

  return params
}

async function resolveVrsAndCall() {
  const vrsConfig = gatewayStore.state.vrs.config
  if (!vrsConfig.phone.trim() || !vrsConfig.fullName.trim()) {
    appendLog('Phone and Full Name are required for VRI', 'error')
    return
  }

  gatewayStore.setState((state) => ({
    ...state,
    vrs: { ...state.vrs, fetchStatus: 'fetching' as VrsFetchStatus },
  }))
  appendLog('Fetching VRI credentials...', 'info')

  try {
    const response = await fetchVrsCredentials(vrsConfig)
    const resolved: PublicCredentials = {
      sipDomain: response.data.domain,
      sipUsername: response.data.ext,
      sipPassword: response.data.secret,
      sipPort: 5060,
    }

    gatewayStore.setState((state) => ({
      ...state,
      vrs: {
        ...state.vrs,
        fetchStatus: 'fetched' as VrsFetchStatus,
        resolvedCredentials: resolved,
      },
    }))
    appendLog(
      `VRI credentials fetched: ${resolved.sipUsername}@${resolved.sipDomain}`,
      'success',
    )

    const params = buildCallParams()
    if (!params) return

    if (!gatewayStore.state.call.sessionId) {
      appendLog(
        'Media session not ready. Preparing automatically before placing call...',
        'info',
      )
      gatewayStore.setState((state) => ({
        ...state,
        pendingCallRequest: params,
      }))
      ensureMediaSessionForCall()
      return
    }

    sendCallPayload(params)
  } catch (error) {
    gatewayStore.setState((state) => ({
      ...state,
      vrs: {
        ...state.vrs,
        fetchStatus: 'error' as VrsFetchStatus,
        resolvedCredentials: null,
      },
    }))
    appendLog(
      `VRI credential fetch failed: ${(error as Error).message}`,
      'error',
    )
  }
}

export function makeCall() {
  if (!isWebSocketOpen(runtime.ws)) {
    appendLog('WebSocket not connected', 'error')
    return
  }

  const destination = gatewayStore.state.controls.destination.trim()
  if (!destination) {
    appendLog('Please enter a destination', 'error')
    return
  }

  if (gatewayStore.state.mode === 'publicvrs') {
    void resolveVrsAndCall()
    return
  }

  const params = buildCallParams()
  if (!params) return

  if (!gatewayStore.state.call.sessionId) {
    appendLog(
      'Media session not ready. Preparing automatically before placing call...',
      'info',
    )
    gatewayStore.setState((state) => ({
      ...state,
      pendingCallRequest: params,
    }))
    ensureMediaSessionForCall()
    return
  }

  sendCallPayload(params)
}

export function hangup() {
  clearRemoteRttState()
  resetOutgoingRttRuntime()

  if (!runtime.activeCallSessionId) {
    appendLog('No active call to hangup', 'warning')
    return
  }

  if (
    gatewayStore.state.call.state === 'ended' ||
    gatewayStore.state.call.state === 'idle'
  ) {
    appendLog('Call already ended, skipping hangup', 'info')
    return
  }

  const sent = sendJson(runtime.ws, {
    type: 'hangup',
    sessionId: runtime.activeCallSessionId,
  })
  if (!sent) {
    appendLog('WebSocket not connected', 'warning')
    return
  }
  appendLog(
    `Sending hangup for sessionId: ${runtime.activeCallSessionId}`,
    'info',
  )
}

export async function resolveTrunk() {
  if (!isWebSocketOpen(runtime.ws)) {
    appendLog('WebSocket not connected', 'error')
    return
  }

  const payload = buildTrunkResolvePayloadFromState()
  if (!payload) {
    appendLog('Please fill in all trunk credentials', 'error')
    return
  }

  runtime.trunkResolvePayload = payload
  runtime.trunkResolvePending = true
  if (payload.trunkId) {
    setTrunkStatus('resolving', `Resolving trunk by ID ${payload.trunkId}...`)
  } else if (payload.trunkPublicId) {
    setTrunkStatus(
      'resolving',
      `Resolving trunk by UUID ${payload.trunkPublicId}...`,
    )
  } else {
    setTrunkStatus(
      'resolving',
      `Resolving trunk: ${payload.sipUsername}@${payload.sipDomain}:${payload.sipPort}...`,
    )
  }

  sendJson(runtime.ws, {
    type: 'trunk_resolve',
    ...runtime.trunkResolvePayload,
  })
}

export function sendPing() {
  const sent = sendJson(runtime.ws, { type: 'ping' })
  if (!sent) {
    appendLog('WebSocket not connected', 'error')
    return
  }
  appendLog('Ping sent', 'info')
}

export function sendDTMF(digits: string) {
  const sessionId = gatewayStore.state.call.sessionId
  if (!sessionId) {
    appendLog('No active call for DTMF', 'warning')
    return
  }
  sendJson(runtime.ws, {
    type: 'dtmf',
    sessionId,
    digits,
  })
  appendLog(`DTMF sent: ${digits}`, 'info')
}

export async function sendSwitch() {
  const sessionId = gatewayStore.state.call.sessionId
  if (!sessionId) {
    appendLog('No active session for switch request', 'warning')
    return
  }

  try {
    await sendSwitchRequest({ sessionId })
    appendLog('Switch request accepted for current session', 'success')
  } catch (error) {
    appendLog(`Switch request failed: ${(error as Error).message}`, 'error')
  }
}

export function acceptCall() {
  const incoming = gatewayStore.state.incomingCall
  if (!incoming?.sessionId) {
    appendLog('Cannot accept call: no active session', 'error')
    return
  }
  if (gatewayStore.state.incomingAction !== 'idle') {
    appendLog('Incoming call action already in progress', 'warning')
    return
  }

  const localSessionId = gatewayStore.state.call.sessionId
  const needMediaPrepare =
    !runtime.pc || !localSessionId || localSessionId === incoming.sessionId

  if (needMediaPrepare) {
    gatewayStore.setState((state) => ({
      ...state,
      incomingAction: 'preparing_accept',
    }))
    runtime.pendingIncomingAcceptSessionId = incoming.sessionId
    appendLog(
      'Incoming call requires local media session. Preparing automatically before accept...',
      'info',
    )
    ensureMediaSessionForCall()
    return
  }

  const sent = sendJson(runtime.ws, { type: 'accept', sessionId: incoming.sessionId })
  if (!sent) {
    gatewayStore.setState((state) => ({
      ...state,
      incomingAction: 'idle',
    }))
    appendLog('WebSocket not connected', 'error')
    return
  }
  runtime.pendingIncomingAcceptSessionId = null
  gatewayStore.setState((state) => ({
    ...state,
    incomingCall: null,
    incomingAction: 'sending_accept',
  }))
  appendLog('Call accepted', 'success')
}

export function rejectCall() {
  if (gatewayStore.state.incomingAction !== 'idle') {
    appendLog('Incoming call is being processed. Reject is temporarily disabled.', 'warning')
    return
  }
  const incoming = gatewayStore.state.incomingCall
  const sessionId = incoming?.sessionId || gatewayStore.state.call.sessionId
  if (!sessionId) {
    appendLog('Cannot reject call: no active session', 'error')
    return
  }

  runtime.pendingIncomingAcceptSessionId = null
  const sent = sendJson(runtime.ws, {
    type: 'reject',
    sessionId,
    reason: 'decline',
  })
  if (!sent) {
    appendLog('WebSocket not connected', 'error')
    return
  }
  gatewayStore.setState((state) => ({
    ...state,
    incomingCall: null,
    incomingAction: 'sending_reject',
  }))
  appendLog('Call rejected', 'info')
}

export function sendSIPMessage(body: string) {
  const trimmed = body.trim()
  if (!trimmed) {
    appendLog('Please enter a message', 'error')
    return false
  }
  if (!isWebSocketOpen(runtime.ws)) {
    appendLog('WebSocket not connected', 'error')
    return false
  }

  sendJson(runtime.ws, {
    type: 'send_message',
    destination: '',
    body: trimmed,
    contentType: 'text/plain;charset=UTF-8',
  })
  void sendOutgoingRttPacket('', 'reset', { resetAfterSend: true })
  appendMessage('You', trimmed, 'outgoing')
  return true
}

export function updateOutgoingRttDraft(draft: string) {
  runtime.outgoingRttPendingText = draft

  if (runtime.outgoingRttTimer) return
  runtime.outgoingRttTimer = setTimeout(
    flushOutgoingRttDraft,
    RTT_SEND_THROTTLE_MS,
  )
}

export function clearLogs() {
  gatewayStore.setState((state) => ({
    ...state,
    logs: [],
  }))
}

export function clearMessages() {
  gatewayStore.setState((state) => ({
    ...state,
    messages: [],
  }))
}

export function toggleDialpad() {
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      dialpadOpen: !state.controls.dialpadOpen,
    },
  }))
}

export async function refreshMediaInputDevices() {
  if (!isBrowser()) return

  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      mediaInputsLoading: true,
    },
  }))

  try {
    const devices = await navigator.mediaDevices.enumerateDevices()
    const availableVideoInputs = mapDeviceOptions(
      devices,
      'videoinput',
      'Camera',
    )
    const availableAudioInputs = mapDeviceOptions(
      devices,
      'audioinput',
      'Microphone',
    )

    const current = gatewayStore.state.controls
    const selectedVideoInputId = fallbackToAvailableInput(
      availableVideoInputs,
      current.selectedVideoInputId,
    )
    const selectedAudioInputId = fallbackToAvailableInput(
      availableAudioInputs,
      current.selectedAudioInputId,
    )

    if (current.selectedVideoInputId && !selectedVideoInputId) {
      appendLog(
        'Selected camera is unavailable, falling back to default',
        'warning',
      )
    }
    if (current.selectedAudioInputId && !selectedAudioInputId) {
      appendLog(
        'Selected microphone is unavailable, falling back to default',
        'warning',
      )
    }

    gatewayStore.setState((state) => ({
      ...state,
      controls: {
        ...state.controls,
        availableVideoInputs,
        availableAudioInputs,
        selectedVideoInputId,
        selectedAudioInputId,
        mediaInputsLoading: false,
      },
    }))
  } catch (error) {
    gatewayStore.setState((state) => ({
      ...state,
      controls: {
        ...state.controls,
        mediaInputsLoading: false,
      },
    }))
    appendLog(
      `Unable to load media devices: ${(error as Error).message}`,
      'warning',
    )
  }
}

async function switchLocalInput(
  kind: 'audio' | 'video',
  selectedDeviceId: string,
) {
  if (!isBrowser()) return

  const switchingKey =
    kind === 'video' ? 'switchingVideoInput' : 'switchingAudioInput'
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      [switchingKey]: true,
    },
  }))

  try {
    const audioDeviceConstraint = selectedDeviceConstraint(selectedDeviceId)
    const stream = await navigator.mediaDevices.getUserMedia(
      kind === 'video'
        ? {
            audio: false,
            video: buildVideoConstraint(selectedDeviceId),
          }
        : {
            audio: audioDeviceConstraint
              ? { deviceId: audioDeviceConstraint }
              : true,
            video: false,
          },
    )

    const newTrack =
      kind === 'video'
        ? stream.getVideoTracks().at(0)
        : stream.getAudioTracks().at(0)

    if (!newTrack) {
      throw new Error(`No ${kind} track available from selected input`)
    }

    const muted =
      kind === 'video'
        ? gatewayStore.state.controls.isMutedVideo
        : gatewayStore.state.controls.isMutedAudio
    newTrack.enabled = !muted

    const pc = runtime.pc
    if (pc) {
      const sender = pc.getSenders().find((s) => s.track?.kind === kind)
      if (sender) {
        await sender.replaceTrack(newTrack)
      } else {
        pc.addTrack(newTrack, new MediaStream([newTrack]))
      }
    }

    updateLocalStreamTrack(kind, newTrack)

    gatewayStore.setState((state) => ({
      ...state,
      media: {
        ...state.media,
        localStream: runtime.localStream,
      },
    }))

    if (runtime.pc && kind === 'video' && runtime.localStream) {
      await applyVideoConstraints(
        runtime.pc,
        runtime.localStream,
        runtime.videoConfig,
      )
    }

    void refreshMediaInputDevices()
    appendLog(
      `Switched ${kind === 'video' ? 'camera' : 'microphone'} input`,
      'success',
    )
  } catch (error) {
    appendLog(
      `Unable to switch ${kind === 'video' ? 'camera' : 'microphone'}: ${(error as Error).message}`,
      'error',
    )
  } finally {
    gatewayStore.setState((state) => ({
      ...state,
      controls: {
        ...state.controls,
        [switchingKey]: false,
      },
    }))
  }
}

export async function setSelectedVideoInput(deviceId: string) {
  const next = deviceId === '__default__' ? '' : deviceId
  if (next === gatewayStore.state.controls.selectedVideoInputId) return

  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      selectedVideoInputId: next,
    },
  }))

  if (runtime.pc && runtime.localStream) {
    await switchLocalInput('video', next)
  }
}

export async function setSelectedAudioInput(deviceId: string) {
  const next = deviceId === '__default__' ? '' : deviceId
  if (next === gatewayStore.state.controls.selectedAudioInputId) return

  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      selectedAudioInputId: next,
    },
  }))

  if (runtime.pc && runtime.localStream) {
    await switchLocalInput('audio', next)
  }
}

export function toggleMuteAudio() {
  const next = !gatewayStore.state.controls.isMutedAudio
  runtime.localStream?.getAudioTracks().forEach((track) => {
    track.enabled = !next
  })
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      isMutedAudio: next,
    },
  }))
  appendLog(next ? 'Audio muted' : 'Audio unmuted', 'info')
}

export function toggleMuteVideo() {
  const next = !gatewayStore.state.controls.isMutedVideo
  runtime.localStream?.getVideoTracks().forEach((track) => {
    track.enabled = !next
  })
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      isMutedVideo: next,
    },
  }))
  appendLog(next ? 'Video muted' : 'Video unmuted', 'info')
}

export function toggleStats() {
  const next = !gatewayStore.state.controls.statsOpen
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      statsOpen: next,
    },
  }))
  if (next) {
    startStats()
  } else {
    stopStats()
  }
}

export function setMode(mode: CallMode) {
  gatewayStore.setState((state) => ({
    ...state,
    mode,
  }))
  // mode is auto-persisted via store subscription
  const modeLabel =
    mode === 'publicvrs' ? 'VRI' : mode === 'siptrunk' ? 'Trunk' : 'SIP'
  appendLog(`Switched to ${modeLabel} mode`, 'info')
}

export function setDestination(destination: string) {
  gatewayStore.setState((state) => ({
    ...state,
    controls: {
      ...state.controls,
      destination,
    },
  }))
}

export function setPublicCredential<TKey extends keyof PublicCredentials>(
  key: TKey,
  value: PublicCredentials[TKey],
) {
  gatewayStore.setState((state) => ({
    ...state,
    publicCredentials: {
      ...state.publicCredentials,
      [key]: value,
    },
  }))
}

export function setTrunkCredential(
  key: keyof GatewayState['trunk']['credentials'],
  value: string | number,
) {
  gatewayStore.setState((state) => ({
    ...state,
    trunk: {
      ...state.trunk,
      credentials: {
        ...state.trunk.credentials,
        [key]: value,
      },
    },
  }))
}

export function setVrsConfigField<TKey extends keyof VrsConfig>(
  key: TKey,
  value: VrsConfig[TKey],
) {
  gatewayStore.setState((state) => ({
    ...state,
    vrs: {
      ...state.vrs,
      config: {
        ...state.vrs.config,
        [key]: value,
      },
      fetchStatus: 'idle' as VrsFetchStatus,
      resolvedCredentials: null,
    },
  }))
}

export function setInputPreset(value: string) {
  setDestination(value)
}

export function initializeGatewayStore() {
  if (!isBrowser() || runtime.initialized) return
  runtime.initialized = true

  // Attach persist subscription (hydrates from localStorage + auto-saves).
  runtime.unsubscribePersist = attachPersist<
    GatewayState,
    PersistedGatewayPrefs
  >(gatewayStore, {
    key: PERSIST_KEY,
    version: PERSIST_VERSION,
    debounceMs: 400,
    select: (state) => ({
      mode: state.mode,
      destination: state.controls.destination,
      publicCredentials: state.publicCredentials,
      trunkCredentials: state.trunk.credentials,
      vrsConfig: state.vrs.config,
      selectedVideoInputId: state.controls.selectedVideoInputId,
      selectedAudioInputId: state.controls.selectedAudioInputId,
    }),
    merge: (persisted, current) => ({
      ...current,
      mode: persisted.mode,
      controls: {
        ...current.controls,
        destination: persisted.destination || current.controls.destination,
        selectedVideoInputId:
          persisted.selectedVideoInputId ??
          current.controls.selectedVideoInputId,
        selectedAudioInputId:
          persisted.selectedAudioInputId ??
          current.controls.selectedAudioInputId,
      },
      publicCredentials: {
        ...current.publicCredentials,
        ...persisted.publicCredentials,
      },
      trunk: {
        ...current.trunk,
        credentials: {
          ...current.trunk.credentials,
          ...persisted.trunkCredentials,
        },
      },
      vrs: {
        ...current.vrs,
        config: {
          ...current.vrs.config,
          ...(persisted.vrsConfig ?? {}),
        },
      },
    }),
  })

  void refreshMediaInputDevices()
  runtime.mediaDeviceChangeHandler = () => {
    void refreshMediaInputDevices()
  }
  navigator.mediaDevices.addEventListener(
    'devicechange',
    runtime.mediaDeviceChangeHandler,
  )

  appendLog('Ready to connect.', 'info')
}

export function cleanupGatewayStore() {
  runtime.manualDisconnect = true
  clearReconnectTimeout()
  stopPingInterval()
  gatewayStore.setState((state) => ({
    ...state,
    connection: {
      status: 'disconnected',
      wsStateText: 'Disconnected',
    },
  }))
  if (runtime.unsubscribePersist) {
    runtime.unsubscribePersist()
    runtime.unsubscribePersist = null
  }
  if (runtime.mediaDeviceChangeHandler) {
    navigator.mediaDevices.removeEventListener(
      'devicechange',
      runtime.mediaDeviceChangeHandler,
    )
    runtime.mediaDeviceChangeHandler = null
  }
  if (runtime.ws) {
    runtime.ws.onopen = null
    runtime.ws.onclose = null
    runtime.ws.onmessage = null
    runtime.ws.onerror = null
    runtime.ws.close()
    runtime.ws = null
  }
  clearResumeRecovery()
  teardownFullSession()
  runtime.pendingRedirectUrl = null
  runtime.resumeRedirectUrl = null
  runtime.trunkResolvePending = false
  runtime.trunkResolvePayload = null
  runtime.pendingIncomingAcceptSessionId = null
  runtime.initialized = false
}

export const gatewayActions = {
  initialize: initializeGatewayStore,
  cleanup: cleanupGatewayStore,
  connect,
  disconnect,
  toggleConnect,
  startSession,
  endSession,
  makeCall,
  hangup,
  resolveTrunk,
  sendPing,
  sendDTMF,
  sendSwitch,
  acceptCall,
  rejectCall,
  sendSIPMessage,
  updateOutgoingRttDraft,
  clearLogs,
  clearMessages,
  toggleDialpad,
  refreshMediaInputDevices,
  setSelectedVideoInput,
  setSelectedAudioInput,
  toggleMuteAudio,
  toggleMuteVideo,
  toggleStats,
  setMode,
  setDestination,
  setPublicCredential,
  setTrunkCredential,
  setVrsConfigField,
  setInputPreset,
}
