import {
  RiAlertLine,
  RiCastLine,
  RiCheckLine,
  RiCheckboxCircleLine,
  RiCloseLine,
  RiComputerLine,
  RiDeleteBinLine,
  RiErrorWarningLine,
  RiInformationLine,
  RiLink,
  RiLinkUnlink,
  RiMicLine,
  RiMicOffLine,
  RiPhoneFill,
  RiPhoneLine,
  RiPlugLine,
  RiRefreshLine,
  RiShareForwardBoxLine,
  RiShutDownLine,
  RiStopCircleLine,
  RiTerminalBoxLine,
  RiVideoOffLine,
  RiVideoOnLine,
} from '@remixicon/react'
import { useCallback, useEffect, useReducer, useRef, useState } from 'react'

import {
  buildAcceptMessage,
  buildAgentRegisterMessage,
  buildCallMessage,
  buildClientStateMessage,
  buildConnectWsUrl,
  buildHangupMessage,
  buildIceMessage,
  buildOfferMessage,
  buildRejectMessage,
  buildRenegotiateAnswerMessage,
  buildStreamUpdateAddMessage,
  buildStreamUpdateRemoveMessage,
  buildTrunkResolveMessage,
  deriveDefaultGatewayWsUrl,
  maskSensitiveUrl,
  parseInboundWsMessage,
  parseSipPort,
  replaceWsUrlEndpoint,
  summarizeWsMessage,
  validateMultiVideoOfferSdp,
  validateStreamAddedOfferSdp,
} from '../protocol'
import {
  INITIAL_MULTI_STREAM_STATE,
  classifyRemoteVideoTrack,
  isLocalScreenSharingOrPending,
  multiStreamReducer,
} from '../state'
import {
  cleanupUncommittedTransceiver,
  detachTransceiverTrack,
  formatTimeStamp,
  preferH264Codecs,
  safeRollbackLocalDescription,
  stopMediaStream,
} from '../webrtc'
import type {
  BannerTone,
  ConnectionFormConfig,
  DiagnosticLogEntry,
  GatewayWsEndpoint,
  InboundWsMessage,
  OutboundWsMessage,
} from '../types'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const MAX_LOG_ENTRIES = 400

const DEFAULT_ICE_SERVERS: Array<RTCIceServer> = [
  { urls: 'stun:stun.l.google.com:19302' },
]

function bannerStyle(tone: BannerTone): string {
  switch (tone) {
    case 'success':
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    case 'warning':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
    case 'error':
      return 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
    default:
      return 'border-cyan-500/30 bg-cyan-500/10 text-cyan-700 dark:text-cyan-300'
  }
}

function BannerIcon({ tone }: { tone: BannerTone }) {
  switch (tone) {
    case 'success':
      return <RiCheckboxCircleLine className="mt-0.5 size-4 shrink-0" />
    case 'warning':
      return <RiAlertLine className="mt-0.5 size-4 shrink-0" />
    case 'error':
      return <RiErrorWarningLine className="mt-0.5 size-4 shrink-0" />
    default:
      return <RiInformationLine className="mt-0.5 size-4 shrink-0" />
  }
}

function connectionBadgeVariant(
  status: string,
): 'success' | 'warning' | 'secondary' {
  if (status === 'registered') return 'success'
  if (status === 'connected' || status === 'connecting') return 'warning'
  return 'secondary'
}

function callBadgeVariant(
  phase: string,
): 'success' | 'warning' | 'destructive' | 'secondary' {
  if (phase === 'active') return 'success'
  if (phase === 'dialing' || phase === 'ringing' || phase === 'incoming') {
    return 'warning'
  }
  if (phase === 'ended') return 'destructive'
  return 'secondary'
}

function VideoTile({
  title,
  badge,
  badgeVariant = 'secondary',
  videoRef,
  hasStream,
  emptyLabel,
  muted = true,
  highlighted = false,
}: {
  title: string
  badge: string
  badgeVariant?:
    | 'default'
    | 'secondary'
    | 'success'
    | 'warning'
    | 'destructive'
    | 'outline'
  videoRef: React.RefObject<HTMLVideoElement | null>
  hasStream: boolean
  emptyLabel: string
  muted?: boolean
  highlighted?: boolean
}) {
  return (
    <div
      className={`flex flex-col overflow-hidden rounded-lg border bg-card ${
        highlighted
          ? 'border-cyan-500/60 ring-1 ring-cyan-500/30'
          : 'border-border'
      }`}
    >
      <div className="flex items-center justify-between border-b border-border/70 px-3 py-1.5">
        <span className="text-xs font-medium">{title}</span>
        <Badge variant={badgeVariant} className="text-[10px]">
          {badge}
        </Badge>
      </div>
      <div className="relative aspect-video w-full bg-zinc-950">
        <video
          ref={videoRef}
          autoPlay
          playsInline
          muted={muted}
          className={`size-full object-contain ${hasStream ? 'opacity-100' : 'opacity-0'}`}
        />
        {!hasStream ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-1.5 px-4 text-center text-xs text-zinc-400">
            <RiComputerLine className="size-6 opacity-40" />
            <span>{emptyLabel}</span>
          </div>
        ) : null}
      </div>
    </div>
  )
}

export function MultiStreamTestPage() {
  const [form, setForm] = useState<ConnectionFormConfig>(() => ({
    endpoint: '/ws-agent',
    wsUrl: deriveDefaultGatewayWsUrl('/ws-agent'),
    accessToken: '',
    sipDomain: '',
    sipUsername: '',
    sipPassword: '',
    sipPort: '5060',
    multiVideo: true,
    multiCall: false,
  }))
  const [destination, setDestination] = useState('')
  const [state, dispatch] = useReducer(
    multiStreamReducer,
    INITIAL_MULTI_STREAM_STATE,
  )
  const [logs, setLogs] = useState<Array<DiagnosticLogEntry>>([])

  const [previewActive, setPreviewActive] = useState(false)
  const [audioMuted, setAudioMuted] = useState(false)
  const [videoMuted, setVideoMuted] = useState(false)
  const [iceState, setIceState] = useState<string>('new')
  const [signalingState, setSignalingState] = useState<string>('stable')
  const [hasRemoteCamera, setHasRemoteCamera] = useState(false)
  const [hasRemoteScreen, setHasRemoteScreen] = useState(false)
  const [hasLocalScreen, setHasLocalScreen] = useState(false)

  // Refs for live browser objects and async handlers
  const wsRef = useRef<WebSocket | null>(null)
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const localMediaRef = useRef<MediaStream | null>(null)
  const localScreenStreamRef = useRef<MediaStream | null>(null)
  const remoteCameraStreamRef = useRef<MediaStream | null>(null)
  const remoteScreenStreamRef = useRef<MediaStream | null>(null)
  const remoteAudioStreamRef = useRef<MediaStream | null>(null)

  const localCameraVideoRef = useRef<HTMLVideoElement | null>(null)
  const localScreenVideoRef = useRef<HTMLVideoElement | null>(null)
  const remoteCameraVideoRef = useRef<HTMLVideoElement | null>(null)
  const remoteScreenVideoRef = useRef<HTMLVideoElement | null>(null)
  const remoteAudioRef = useRef<HTMLAudioElement | null>(null)

  const screenSenderTransceiverRef = useRef<RTCRtpTransceiver | null>(null)
  const screenRecvTransceiverRef = useRef<RTCRtpTransceiver | null>(null)
  const cameraTransceiverMidRef = useRef<string | null>(null)
  const pendingRenegotiationRef = useRef<{
    renegotiationId: string
    transceiver: RTCRtpTransceiver
  } | null>(null)

  const pendingDestinationRef = useRef<string | null>(null)
  const pendingAcceptSessionIdRef = useRef<string | null>(null)
  const pendingIceCandidatesRef = useRef<Array<RTCIceCandidateInit>>([])
  const pendingStreamActionRef = useRef<
    'add_with_sdp' | 'add_reuse' | 'remove' | null
  >(null)
  const uncommittedScreenOfferRef = useRef<RTCRtpTransceiver | null>(null)
  const sessionIdRef = useRef<string | null>(null)
  const stateRef = useRef(state)
  const formRef = useRef(form)
  const logIdRef = useRef(1)
  const logContainerRef = useRef<HTMLDivElement | null>(null)

  stateRef.current = state
  formRef.current = form
  sessionIdRef.current = state.sessionId

  const appendLog = useCallback(
    (direction: 'IN' | 'OUT' | 'SYS', type: string, summary: string) => {
      const entry: DiagnosticLogEntry = {
        id: logIdRef.current++,
        timestamp: formatTimeStamp(),
        direction,
        type,
        summary,
      }
      setLogs((prev) => {
        const next = [...prev, entry]
        return next.length > MAX_LOG_ENTRIES
          ? next.slice(next.length - MAX_LOG_ENTRIES)
          : next
      })
    },
    [],
  )

  useEffect(() => {
    const el = logContainerRef.current
    if (el) {
      el.scrollTop = el.scrollHeight
    }
  }, [logs])

  const bindVideoElement = useCallback(
    (
      ref: React.RefObject<HTMLVideoElement | null>,
      stream: MediaStream | null,
    ) => {
      const el = ref.current
      if (!el) return
      if (el.srcObject !== stream) {
        el.srcObject = stream
      }
      if (stream) {
        void el.play().catch(() => {
          // Autoplay may require gesture; muted video normally succeeds.
        })
      }
    },
    [],
  )

  const bindAudioElement = useCallback((stream: MediaStream | null) => {
    const el = remoteAudioRef.current
    if (!el) return
    if (el.srcObject !== stream) {
      el.srcObject = stream
    }
    if (stream) {
      void el.play().catch(() => {
        // Ignore autoplay restrictions before user interaction.
      })
    }
  }, [])

  const sendWs = useCallback(
    (msg: OutboundWsMessage): boolean => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        appendLog(
          'SYS',
          'ws_not_open',
          `Cannot send ${msg.type}: socket closed`,
        )
        return false
      }
      ws.send(JSON.stringify(msg))
      appendLog('OUT', msg.type, summarizeWsMessage(msg))
      return true
    },
    [appendLog],
  )

  const stopLocalScreenCapture = useCallback(() => {
    const stream = localScreenStreamRef.current
    if (stream) {
      for (const track of stream.getTracks()) {
        track.onended = null
      }
      stopMediaStream(stream)
      localScreenStreamRef.current = null
    }
    bindVideoElement(localScreenVideoRef, null)
    setHasLocalScreen(false)
  }, [bindVideoElement])

  const cleanupPeerConnection = useCallback(() => {
    stopLocalScreenCapture()

    if (pcRef.current) {
      pcRef.current.onicecandidate = null
      pcRef.current.ontrack = null
      pcRef.current.oniceconnectionstatechange = null
      pcRef.current.onsignalingstatechange = null
      try {
        pcRef.current.close()
      } catch {
        // Ignore close errors.
      }
      pcRef.current = null
    }

    screenSenderTransceiverRef.current = null
    screenRecvTransceiverRef.current = null
    cameraTransceiverMidRef.current = null
    pendingRenegotiationRef.current = null
    pendingDestinationRef.current = null
    pendingAcceptSessionIdRef.current = null
    pendingIceCandidatesRef.current = []
    pendingStreamActionRef.current = null
    uncommittedScreenOfferRef.current = null

    remoteCameraStreamRef.current = null
    remoteScreenStreamRef.current = null
    remoteAudioStreamRef.current = null
    bindVideoElement(remoteCameraVideoRef, null)
    bindVideoElement(remoteScreenVideoRef, null)
    bindAudioElement(null)
    setHasRemoteCamera(false)
    setHasRemoteScreen(false)
    setIceState('new')
    setSignalingState('stable')
  }, [bindAudioElement, bindVideoElement, stopLocalScreenCapture])

  const ensureLocalCameraStream =
    useCallback(async (): Promise<MediaStream> => {
      const existing = localMediaRef.current
      if (
        existing &&
        existing.getTracks().some((t) => t.readyState === 'live')
      ) {
        return existing
      }
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: true,
        video: { width: { ideal: 1280 }, height: { ideal: 720 } },
      })
      for (const track of stream.getAudioTracks()) {
        track.enabled = !audioMuted
      }
      for (const track of stream.getVideoTracks()) {
        track.enabled = !videoMuted
      }
      localMediaRef.current = stream
      bindVideoElement(localCameraVideoRef, stream)
      setPreviewActive(true)
      appendLog('SYS', 'media', 'Camera and microphone acquired')
      return stream
    }, [appendLog, audioMuted, bindVideoElement, videoMuted])

  const togglePreview = useCallback(async () => {
    if (previewActive && localMediaRef.current) {
      if (pcRef.current) {
        appendLog(
          'SYS',
          'media',
          'Hang up active call before stopping camera preview',
        )
        return
      }
      stopMediaStream(localMediaRef.current)
      localMediaRef.current = null
      bindVideoElement(localCameraVideoRef, null)
      setPreviewActive(false)
      appendLog('SYS', 'media', 'Local camera preview stopped')
      return
    }
    try {
      await ensureLocalCameraStream()
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to access camera/mic'
      dispatch({ type: 'WS_ERROR', error: message })
      appendLog('SYS', 'media_error', message)
    }
  }, [appendLog, bindVideoElement, ensureLocalCameraStream, previewActive])

  const toggleAudioMute = useCallback(() => {
    setAudioMuted((prev) => {
      const next = !prev
      if (localMediaRef.current) {
        for (const track of localMediaRef.current.getAudioTracks()) {
          track.enabled = !next
        }
      }
      return next
    })
  }, [])

  const toggleVideoMute = useCallback(() => {
    setVideoMuted((prev) => {
      const next = !prev
      if (localMediaRef.current) {
        for (const track of localMediaRef.current.getVideoTracks()) {
          track.enabled = !next
        }
      }
      return next
    })
  }, [])

  const createPeerConnection = useCallback(
    (localStream: MediaStream): RTCPeerConnection => {
      cleanupPeerConnection()

      const pc = new RTCPeerConnection({ iceServers: DEFAULT_ICE_SERVERS })
      pcRef.current = pc
      setIceState(pc.iceConnectionState)
      setSignalingState(pc.signalingState)

      pc.oniceconnectionstatechange = () => {
        setIceState(pc.iceConnectionState)
        appendLog(
          'SYS',
          'ice_state',
          `iceConnectionState=${pc.iceConnectionState}`,
        )
      }

      pc.onsignalingstatechange = () => {
        setSignalingState(pc.signalingState)
      }

      pc.onicecandidate = (event) => {
        const candidate = event.candidate
        if (!candidate || !candidate.candidate) return
        const init = candidate.toJSON()
        const sid = sessionIdRef.current
        if (sid) {
          sendWs(buildIceMessage(sid, init))
        } else {
          pendingIceCandidatesRef.current.push(init)
        }
      }

      pc.ontrack = (event) => {
        const track = event.track
        if (track.kind === 'audio') {
          const audioStream = new MediaStream([track])
          remoteAudioStreamRef.current = audioStream
          bindAudioElement(audioStream)
          appendLog('SYS', 'remote_track', `kind=audio id=${track.id}`)
          return
        }

        if (track.kind === 'video') {
          const videoTransceivers = pc
            .getTransceivers()
            .filter((t) => t.receiver.track.kind === 'video')
          const videoTransceiverIndex = videoTransceivers.indexOf(
            event.transceiver,
          )
          const role = classifyRemoteVideoTrack({
            trackId: track.id,
            transceiverMid: event.transceiver.mid,
            screenRecvMid: screenRecvTransceiverRef.current?.mid ?? null,
            isKnownScreenTransceiver:
              screenRecvTransceiverRef.current === event.transceiver,
            videoTransceiverIndex,
            hasCameraVideoTrack: remoteCameraStreamRef.current !== null,
            cameraTransceiverMid: cameraTransceiverMidRef.current,
          })

          appendLog(
            'SYS',
            'remote_track',
            `kind=video role=${role} id=${track.id} mid=${event.transceiver.mid ?? '?'} txIndex=${videoTransceiverIndex}`,
          )

          if (role === 'screen') {
            const screenStream = new MediaStream([track])
            remoteScreenStreamRef.current = screenStream
            bindVideoElement(remoteScreenVideoRef, screenStream)
            setHasRemoteScreen(true)
          } else {
            cameraTransceiverMidRef.current = event.transceiver.mid
            const camStream = new MediaStream([track])
            remoteCameraStreamRef.current = camStream
            bindVideoElement(remoteCameraVideoRef, camStream)
            setHasRemoteCamera(true)
          }
        }
      }

      const audioTrack = localStream.getAudioTracks().at(0)
      if (audioTrack) {
        pc.addTransceiver(audioTrack, {
          direction: 'sendrecv',
          streams: [localStream],
        })
      } else {
        pc.addTransceiver('audio', { direction: 'sendrecv' })
      }

      const videoTrack = localStream.getVideoTracks().at(0)
      const cameraTx = videoTrack
        ? pc.addTransceiver(videoTrack, {
            direction: 'sendrecv',
            streams: [localStream],
          })
        : pc.addTransceiver('video', { direction: 'sendrecv' })
      preferH264Codecs(cameraTx, 'send')

      return pc
    },
    [
      appendLog,
      bindAudioElement,
      bindVideoElement,
      cleanupPeerConnection,
      sendWs,
    ],
  )

  const stopScreenShare = useCallback(async () => {
    stopLocalScreenCapture()

    // Keep the screen transceiver intact for fast re-share; detach track only.
    await detachTransceiverTrack(screenSenderTransceiverRef.current)

    const current = stateRef.current
    const sid = current.sessionId
    const wasSharing =
      current.localScreenStatus === 'negotiating' ||
      current.localScreenStatus === 'waiting_peer' ||
      current.localScreenStatus === 'active'

    if (sid && current.callPhase === 'active' && wasSharing) {
      pendingStreamActionRef.current = 'remove'
      dispatch({ type: 'LOCAL_SCREEN_STOP_START' })
      sendWs(buildStreamUpdateRemoveMessage(sid))
      return
    }

    pendingStreamActionRef.current = null
    dispatch({ type: 'LOCAL_SCREEN_STOP_COMPLETE' })
  }, [sendWs, stopLocalScreenCapture])

  const stopScreenShareRef = useRef(stopScreenShare)
  stopScreenShareRef.current = stopScreenShare

  const startScreenShare = useCallback(async () => {
    const current = stateRef.current
    const pc = pcRef.current
    const sid = current.sessionId
    if (!pc || !sid || current.callPhase !== 'active') {
      appendLog(
        'SYS',
        'screen_share',
        'Active call is required to share screen',
      )
      return
    }
    if (!current.multiVideoEnabled) {
      dispatch({
        type: 'LOCAL_SCREEN_LOCAL_ERROR',
        reason: 'multi_video_not_enabled',
        message:
          'multiVideo is disabled on this connection. Enable multiVideo and reconnect.',
      })
      return
    }

    let screenStream: MediaStream
    try {
      const CaptureControllerCtor = (
        globalThis as typeof globalThis & {
          CaptureController?: new () => {
            setFocusBehavior?: (behavior: 'focus-captured-surface' | 'no-focus-change') => void
          }
        }
      ).CaptureController
      const controller =
        typeof CaptureControllerCtor === 'function'
          ? new CaptureControllerCtor()
          : undefined
      screenStream = await navigator.mediaDevices.getDisplayMedia({
        video: {
          width: { max: 1920 },
          height: { max: 1080 },
          frameRate: { ideal: 15, max: 30 },
        },
        audio: false,
        ...(controller ? { controller } : {}),
      } as DisplayMediaStreamOptions)
      try {
        controller?.setFocusBehavior?.('no-focus-change')
      } catch {
        // Only applies to tab/window captures; ignored for entire screen.
      }
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Screen capture cancelled'
      appendLog('SYS', 'screen_capture_cancelled', message)
      return
    }

    const screenTrack = screenStream.getVideoTracks().at(0)
    if (!screenTrack) {
      stopMediaStream(screenStream)
      dispatch({
        type: 'LOCAL_SCREEN_LOCAL_ERROR',
        message: 'Display capture returned no video track.',
      })
      return
    }

    localScreenStreamRef.current = screenStream
    bindVideoElement(localScreenVideoRef, screenStream)
    setHasLocalScreen(true)

    // Automatically trigger stop share when the user clicks the browser "Stop sharing" bar.
    screenTrack.onended = () => {
      appendLog('SYS', 'screen_track_ended', 'Browser stop-sharing bar pressed')
      void stopScreenShareRef.current()
    }

    const existingTx = screenSenderTransceiverRef.current
    const canReuse =
      current.localScreenTransceiverNegotiated &&
      existingTx !== null &&
      existingTx.mid !== null

    if (canReuse) {
      try {
        await existingTx.sender.replaceTrack(screenTrack)
        pendingStreamActionRef.current = 'add_reuse'
        dispatch({ type: 'LOCAL_SCREEN_START', reusedTransceiver: true })
        if (!sendWs(buildStreamUpdateAddMessage(sid))) {
          pendingStreamActionRef.current = null
          await detachTransceiverTrack(existingTx)
          throw new Error('WebSocket is not open; cannot send stream_update')
        }
      } catch (err) {
        stopLocalScreenCapture()
        const message =
          err instanceof Error ? err.message : 'Failed to attach screen track'
        dispatch({ type: 'LOCAL_SCREEN_LOCAL_ERROR', message })
        appendLog('SYS', 'screen_share_error', message)
      }
      return
    }

    // First share on this call: add 2nd sendonly video transceiver on the LIVE PC.
    let addedTx: RTCRtpTransceiver | null = null
    try {
      addedTx = pc.addTransceiver(screenTrack, {
        direction: 'sendonly',
        streams: [screenStream],
      })
      preferH264Codecs(addedTx, 'send')
      screenSenderTransceiverRef.current = addedTx

      const offer = await pc.createOffer()
      const offerSdp = offer.sdp ?? ''
      const validation = validateMultiVideoOfferSdp(offerSdp)
      if (!validation.valid) {
        await cleanupUncommittedTransceiver(addedTx)
        screenSenderTransceiverRef.current = null
        stopLocalScreenCapture()
        dispatch({
          type: 'LOCAL_SCREEN_LOCAL_ERROR',
          reason: 'invalid_offer',
          message: validation.error ?? 'Offer must contain 2 video m-lines.',
        })
        appendLog('SYS', 'screen_offer_invalid', validation.error ?? 'invalid')
        return
      }

      uncommittedScreenOfferRef.current = addedTx
      await pc.setLocalDescription(offer)
      pendingStreamActionRef.current = 'add_with_sdp'
      dispatch({ type: 'LOCAL_SCREEN_START', reusedTransceiver: false })
      if (!sendWs(buildStreamUpdateAddMessage(sid, offerSdp))) {
        throw new Error('WebSocket is not open; cannot send stream_update')
      }
    } catch (err) {
      await safeRollbackLocalDescription(pc)
      await cleanupUncommittedTransceiver(addedTx)
      if (screenSenderTransceiverRef.current === addedTx) {
        screenSenderTransceiverRef.current = null
      }
      if (uncommittedScreenOfferRef.current === addedTx) {
        uncommittedScreenOfferRef.current = null
      }
      pendingStreamActionRef.current = null
      stopLocalScreenCapture()
      const message =
        err instanceof Error
          ? err.message
          : 'Failed to negotiate screen share offer'
      dispatch({ type: 'LOCAL_SCREEN_LOCAL_ERROR', message })
      appendLog('SYS', 'screen_share_error', message)
    }
  }, [appendLog, bindVideoElement, sendWs, stopLocalScreenCapture])

  const handleInboundRenegotiate = useCallback(
    async (msg: InboundWsMessage) => {
      if (
        msg.reason !== 'stream_added' ||
        (msg.role && msg.role !== 'screen')
      ) {
        appendLog(
          'SYS',
          'renegotiate_ignored',
          `Unhandled renegotiate reason=${msg.reason ?? 'unknown'}`,
        )
        return
      }
      const sid = msg.sessionId
      const renId = msg.renegotiationId
      const pc = pcRef.current
      if (!sid || !renId || !pc) {
        if (sid && renId) {
          sendWs(
            buildRenegotiateAnswerMessage({
              sessionId: sid,
              renegotiationId: renId,
              status: 'failed',
              reason: 'no_peer_connection',
            }),
          )
        }
        return
      }

      let recvTx: RTCRtpTransceiver | null = null
      try {
        // Count existing video transceivers first: if this peer previously shared
        // its own screen (kept sendonly transceiver), adding recvonly yields 3 m=video lines.
        const previousVideoCount = pc
          .getTransceivers()
          .filter((t) => t.receiver.track.kind === 'video').length

        // Add a new recvonly video transceiver on the LIVE PeerConnection (no PC replacement).
        recvTx = pc.addTransceiver('video', { direction: 'recvonly' })
        preferH264Codecs(recvTx, 'recv')
        screenRecvTransceiverRef.current = recvTx

        const offer = await pc.createOffer()
        const offerSdp = offer.sdp ?? ''
        const validation = validateStreamAddedOfferSdp(
          offerSdp,
          Math.max(2, previousVideoCount + 1),
        )
        if (!validation.valid) {
          await cleanupUncommittedTransceiver(recvTx)
          screenRecvTransceiverRef.current = null
          sendWs(
            buildRenegotiateAnswerMessage({
              sessionId: sid,
              renegotiationId: renId,
              status: 'failed',
              reason: 'invalid_offer',
            }),
          )
          appendLog(
            'SYS',
            'renegotiate_error',
            validation.error ?? 'Invalid stream_added offer',
          )
          return
        }

        await pc.setLocalDescription(offer)
        pendingRenegotiationRef.current = {
          renegotiationId: renId,
          transceiver: recvTx,
        }
        const sent = sendWs(
          buildRenegotiateAnswerMessage({
            sessionId: sid,
            renegotiationId: renId,
            status: 'ok',
            sdp: offerSdp,
          }),
        )
        if (!sent) {
          pendingRenegotiationRef.current = null
          throw new Error(
            'WebSocket is not open; cannot send renegotiate_answer',
          )
        }
      } catch (err) {
        await safeRollbackLocalDescription(pc)
        await cleanupUncommittedTransceiver(recvTx)
        if (screenRecvTransceiverRef.current === recvTx) {
          screenRecvTransceiverRef.current = null
        }
        if (pendingRenegotiationRef.current?.transceiver === recvTx) {
          pendingRenegotiationRef.current = null
        }
        const message =
          err instanceof Error ? err.message : 'renegotiate_failed'
        sendWs(
          buildRenegotiateAnswerMessage({
            sessionId: sid,
            renegotiationId: renId,
            status: 'failed',
            reason: message,
          }),
        )
        appendLog('SYS', 'renegotiate_error', message)
      }
    },
    [appendLog, sendWs],
  )

  const handleWsMessage = useCallback(
    async (raw: string) => {
      const msg = parseInboundWsMessage(raw)
      if (!msg) {
        appendLog('IN', 'unparsed', raw.slice(0, 160))
        return
      }
      appendLog('IN', msg.type, summarizeWsMessage(msg))

      // Snapshot state before reducer updates it
      const stateBefore = stateRef.current
      dispatch({ type: 'INBOUND_WS', message: msg })

      if (msg.type === 'answer') {
        const pc = pcRef.current
        const sid = msg.sessionId
        if (sid) {
          sessionIdRef.current = sid
          if (pendingIceCandidatesRef.current.length > 0) {
            for (const candidate of pendingIceCandidatesRef.current) {
              sendWs(buildIceMessage(sid, candidate))
            }
            pendingIceCandidatesRef.current = []
          }
        }
        if (pc && msg.sdp) {
          try {
            await pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp })
          } catch (err) {
            const message =
              err instanceof Error ? err.message : 'Failed to apply SDP answer'
            dispatch({ type: 'WS_ERROR', error: message })
            appendLog('SYS', 'sdp_error', message)
            return
          }
        }

        if (sid && pendingAcceptSessionIdRef.current === sid) {
          pendingAcceptSessionIdRef.current = null
          sendWs(buildAcceptMessage(sid))
        } else if (sid && pendingDestinationRef.current) {
          const dest = pendingDestinationRef.current
          pendingDestinationRef.current = null
          const currentForm = formRef.current
          const shouldAttachSipCreds =
            stateRef.current.connectionStatus !== 'registered' &&
            Boolean(
              currentForm.sipDomain.trim() &&
              currentForm.sipUsername.trim() &&
              currentForm.sipPassword,
            )
          sendWs(
            buildCallMessage({
              sessionId: sid,
              destination: dest,
              sipCredentials: shouldAttachSipCreds
                ? {
                    sipDomain: currentForm.sipDomain,
                    sipUsername: currentForm.sipUsername,
                    sipPassword: currentForm.sipPassword,
                    sipPort: parseSipPort(currentForm.sipPort),
                  }
                : undefined,
            }),
          )
        }
        return
      }

      if (msg.type === 'state' && msg.state === 'ended') {
        if (
          !sessionIdRef.current ||
          !msg.sessionId ||
          msg.sessionId === sessionIdRef.current
        ) {
          cleanupPeerConnection()
        }
        return
      }

      if (msg.type === 'stream_update_result') {
        pendingStreamActionRef.current = null
        const pc = pcRef.current
        if (msg.action === 'add') {
          if (msg.status === 'ok') {
            uncommittedScreenOfferRef.current = null
            if (msg.sdp && pc) {
              try {
                await pc.setRemoteDescription({
                  type: 'answer',
                  sdp: msg.sdp,
                })
                appendLog(
                  'SYS',
                  'screen_sdp_applied',
                  'Applied gateway answer SDP for screen transceiver',
                )
              } catch (err) {
                const message =
                  err instanceof Error
                    ? err.message
                    : 'Failed to apply screen share answer SDP'
                appendLog('SYS', 'sdp_error', message)
              }
            }
          } else {
            // Rejected (e.g. not_supported) or failed: rollback any uncommitted offer even if stopScreenShare ran while in flight.
            const uncommittedTx = uncommittedScreenOfferRef.current
            const needsRollback =
              uncommittedTx !== null ||
              pc?.signalingState === 'have-local-offer'
            if (needsRollback) {
              const rolledBack = await safeRollbackLocalDescription(pc)
              const targetTx =
                uncommittedTx ?? screenSenderTransceiverRef.current
              await cleanupUncommittedTransceiver(targetTx)
              if (
                screenSenderTransceiverRef.current === targetTx ||
                screenSenderTransceiverRef.current?.mid === null
              ) {
                screenSenderTransceiverRef.current = null
              }
              uncommittedScreenOfferRef.current = null
              appendLog(
                'SYS',
                'screen_rollback',
                `Rolled back local offer (${rolledBack ? 'ok' : 'skipped'}) after ${msg.status}:${msg.reason ?? ''}`,
              )
            } else {
              await detachTransceiverTrack(screenSenderTransceiverRef.current)
            }
            stopLocalScreenCapture()
          }
        }
        return
      }

      if (msg.type === 'stream_state' && (!msg.role || msg.role === 'screen')) {
        if (msg.active === false) {
          if (localScreenStreamRef.current) {
            stopLocalScreenCapture()
            await detachTransceiverTrack(screenSenderTransceiverRef.current)
            appendLog(
              'SYS',
              'screen_stopped',
              `Stopped local screen capture (reason=${msg.reason ?? 'closed'})`,
            )
          }
          if (msg.reason === 'agent_switch' || msg.reason === 'resume') {
            screenSenderTransceiverRef.current = null
            screenRecvTransceiverRef.current = null
            uncommittedScreenOfferRef.current = null
          }
          if (
            msg.reason === 'peer_closed' ||
            msg.reason === 'agent_switch' ||
            msg.reason === 'resume'
          ) {
            setHasRemoteScreen(false)
          }
        } else if (
          msg.active === true &&
          !isLocalScreenSharingOrPending(stateBefore.localScreenStatus) &&
          remoteScreenStreamRef.current
        ) {
          setHasRemoteScreen(true)
          bindVideoElement(remoteScreenVideoRef, remoteScreenStreamRef.current)
        }
        return
      }

      if (msg.type === 'renegotiate') {
        await handleInboundRenegotiate(msg)
        return
      }

      if (msg.type === 'renegotiate_result') {
        const pending = pendingRenegotiationRef.current
        if (
          pending &&
          (!msg.renegotiationId ||
            msg.renegotiationId === pending.renegotiationId)
        ) {
          pendingRenegotiationRef.current = null
          const pc = pcRef.current
          if (msg.status === 'ok' && msg.sdp && pc) {
            try {
              await pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp })
              appendLog(
                'SYS',
                'renegotiate_applied',
                'Applied renegotiate_result answer SDP for incoming screen',
              )
            } catch (err) {
              const message =
                err instanceof Error
                  ? err.message
                  : 'Failed to apply renegotiate_result SDP'
              appendLog('SYS', 'renegotiate_error', message)
            }
          } else if (msg.status !== 'ok') {
            await safeRollbackLocalDescription(pc)
            await cleanupUncommittedTransceiver(pending.transceiver)
            if (screenRecvTransceiverRef.current === pending.transceiver) {
              screenRecvTransceiverRef.current = null
            }
            appendLog(
              'SYS',
              'renegotiate_rollback',
              `Rolled back recvonly screen transceiver (${msg.status}:${msg.reason ?? ''})`,
            )
          }
        }
      }
    },
    [
      appendLog,
      bindVideoElement,
      cleanupPeerConnection,
      handleInboundRenegotiate,
      sendWs,
      stopLocalScreenCapture,
    ],
  )

  const handleWsMessageRef = useRef(handleWsMessage)
  handleWsMessageRef.current = handleWsMessage

  const registerOrSendCapabilities = useCallback(() => {
    const currentForm = formRef.current
    if (currentForm.endpoint === '/ws-agent') {
      if (
        !currentForm.sipDomain.trim() ||
        !currentForm.sipUsername.trim() ||
        !currentForm.sipPassword
      ) {
        appendLog(
          'SYS',
          'register_skipped',
          'Fill SIP domain, username, and password to send agent_register',
        )
        return
      }
      if (
        sendWs(
          buildAgentRegisterMessage({
            sipDomain: currentForm.sipDomain,
            sipUsername: currentForm.sipUsername,
            sipPassword: currentForm.sipPassword,
            sipPort: parseSipPort(currentForm.sipPort),
            multiCall: currentForm.multiCall,
            multiVideo: currentForm.multiVideo,
          }),
        )
      ) {
        dispatch({
          type: 'LOCAL_CAPABILITIES_SENT',
          multiVideo: currentForm.multiVideo,
          multiCall: currentForm.multiCall,
        })
      }
      return
    }

    // /ws endpoint: opt in via client_state and optionally resolve trunk if SIP credentials are supplied.
    if (
      sendWs(
        buildClientStateMessage({
          availability: 'idle',
          multiCall: currentForm.multiCall,
          multiVideo: currentForm.multiVideo,
        }),
      )
    ) {
      dispatch({
        type: 'LOCAL_CAPABILITIES_SENT',
        multiVideo: currentForm.multiVideo,
        multiCall: currentForm.multiCall,
      })
    }
    if (
      currentForm.sipDomain.trim() &&
      currentForm.sipUsername.trim() &&
      currentForm.sipPassword
    ) {
      sendWs(
        buildTrunkResolveMessage({
          sipDomain: currentForm.sipDomain,
          sipUsername: currentForm.sipUsername,
          sipPassword: currentForm.sipPassword,
          sipPort: parseSipPort(currentForm.sipPort),
        }),
      )
    }
  }, [appendLog, sendWs])

  const disconnect = useCallback(() => {
    cleanupPeerConnection()
    if (wsRef.current) {
      wsRef.current.onopen = null
      wsRef.current.onmessage = null
      wsRef.current.onerror = null
      wsRef.current.onclose = null
      try {
        wsRef.current.close(1000, 'user_disconnect')
      } catch {
        // Ignore close errors.
      }
      wsRef.current = null
    }
    dispatch({ type: 'WS_CLOSED' })
    appendLog('SYS', 'ws_close', 'Disconnected by user')
  }, [appendLog, cleanupPeerConnection])

  const connect = useCallback(() => {
    const currentForm = formRef.current
    if (!currentForm.wsUrl.trim()) {
      dispatch({ type: 'WS_ERROR', error: 'WebSocket URL is required' })
      return
    }
    if (wsRef.current) {
      wsRef.current.onopen = null
      wsRef.current.onmessage = null
      wsRef.current.onerror = null
      wsRef.current.onclose = null
      try {
        wsRef.current.close()
      } catch {
        // Ignore previous socket close errors.
      }
      wsRef.current = null
    }

    const fullUrl = buildConnectWsUrl(
      currentForm.wsUrl,
      currentForm.accessToken,
    )
    dispatch({
      type: 'WS_CONNECTING',
      multiVideo: currentForm.multiVideo,
      multiCall: currentForm.multiCall,
    })
    appendLog('SYS', 'ws_connect', `Connecting to ${maskSensitiveUrl(fullUrl)}`)

    let ws: WebSocket
    try {
      ws = new WebSocket(fullUrl)
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Invalid WebSocket URL'
      dispatch({ type: 'WS_ERROR', error: message })
      dispatch({ type: 'WS_CLOSED', reason: message })
      appendLog('SYS', 'ws_error', message)
      return
    }

    wsRef.current = ws

    ws.onopen = () => {
      dispatch({ type: 'WS_OPEN' })
      appendLog('SYS', 'ws_open', `Connected (${currentForm.endpoint})`)
      registerOrSendCapabilities()
    }

    ws.onmessage = (event) => {
      if (typeof event.data === 'string') {
        void handleWsMessageRef.current(event.data)
      }
    }

    ws.onerror = () => {
      dispatch({ type: 'WS_ERROR', error: 'WebSocket transport error' })
      appendLog('SYS', 'ws_error', 'WebSocket transport error')
    }

    ws.onclose = (event) => {
      if (wsRef.current === ws) {
        wsRef.current = null
      }
      cleanupPeerConnection()
      const reason = event.reason || `code ${event.code}`
      dispatch({ type: 'WS_CLOSED', reason })
      appendLog('SYS', 'ws_close', `Closed (${reason})`)
    }
  }, [appendLog, cleanupPeerConnection, registerOrSendCapabilities])

  const placeCall = useCallback(async () => {
    const dest = destination.trim()
    if (!dest) {
      dispatch({ type: 'WS_ERROR', error: 'Enter a SIP destination to dial' })
      return
    }
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      dispatch({ type: 'WS_ERROR', error: 'Connect WebSocket first' })
      return
    }

    try {
      const localStream = await ensureLocalCameraStream()
      const pc = createPeerConnection(localStream)
      pendingDestinationRef.current = dest
      pendingAcceptSessionIdRef.current = null
      dispatch({ type: 'LOCAL_CALL_START' })

      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      if (!offer.sdp) {
        throw new Error('Browser created an empty SDP offer')
      }
      sendWs(buildOfferMessage(offer.sdp))
    } catch (err) {
      cleanupPeerConnection()
      dispatch({ type: 'LOCAL_CALL_RESET' })
      const message =
        err instanceof Error ? err.message : 'Failed to start outbound call'
      dispatch({ type: 'WS_ERROR', error: message })
      appendLog('SYS', 'call_error', message)
    }
  }, [
    appendLog,
    cleanupPeerConnection,
    createPeerConnection,
    destination,
    ensureLocalCameraStream,
    sendWs,
  ])

  const acceptIncomingCall = useCallback(async () => {
    const incoming = stateRef.current.incomingCall
    if (!incoming) return
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return

    try {
      const localStream = await ensureLocalCameraStream()
      const pc = createPeerConnection(localStream)
      pendingDestinationRef.current = null
      pendingAcceptSessionIdRef.current = incoming.sessionId
      sessionIdRef.current = incoming.sessionId
      dispatch({
        type: 'LOCAL_INCOMING_ACCEPT_START',
        sessionId: incoming.sessionId,
      })

      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      if (!offer.sdp) {
        throw new Error('Browser created an empty SDP offer')
      }
      sendWs(buildOfferMessage(offer.sdp, incoming.sessionId))
    } catch (err) {
      cleanupPeerConnection()
      dispatch({ type: 'LOCAL_CALL_RESET' })
      const message =
        err instanceof Error ? err.message : 'Failed to accept incoming call'
      dispatch({ type: 'WS_ERROR', error: message })
      appendLog('SYS', 'accept_error', message)
    }
  }, [
    appendLog,
    cleanupPeerConnection,
    createPeerConnection,
    ensureLocalCameraStream,
    sendWs,
  ])

  const rejectIncomingCall = useCallback(() => {
    const incoming = stateRef.current.incomingCall
    if (!incoming) return
    sendWs(buildRejectMessage(incoming.sessionId, 'busy'))
    dispatch({ type: 'LOCAL_CALL_RESET' })
  }, [sendWs])

  const hangupCall = useCallback(() => {
    const sid = stateRef.current.sessionId
    if (sid) {
      sendWs(buildHangupMessage(sid, 'user_hangup'))
    }
    cleanupPeerConnection()
    dispatch({ type: 'LOCAL_CALL_RESET' })
  }, [cleanupPeerConnection, sendWs])

  // Cleanup all media and sockets on page unmount
  useEffect(() => {
    return () => {
      cleanupPeerConnection()
      stopMediaStream(localMediaRef.current)
      localMediaRef.current = null
      if (wsRef.current) {
        try {
          wsRef.current.close(1000, 'page_unmount')
        } catch {
          // Ignore close errors.
        }
        wsRef.current = null
      }
    }
  }, [cleanupPeerConnection])

  const isConnected =
    state.connectionStatus === 'connected' ||
    state.connectionStatus === 'registered'
  const isConnecting = state.connectionStatus === 'connecting'
  const isInCall =
    state.callPhase === 'dialing' ||
    state.callPhase === 'ringing' ||
    state.callPhase === 'active'
  const isSharingScreen =
    state.localScreenStatus === 'negotiating' ||
    state.localScreenStatus === 'waiting_peer' ||
    state.localScreenStatus === 'active'

  const handleEndpointChange = (endpoint: GatewayWsEndpoint) => {
    setForm((prev) => ({
      ...prev,
      endpoint,
      wsUrl: replaceWsUrlEndpoint(prev.wsUrl, endpoint),
    }))
  }

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <Badge
          variant={connectionBadgeVariant(state.connectionStatus)}
          className="text-[11px]"
        >
          {state.connectionStatus}
        </Badge>
        {state.multiVideoEnabled && isConnected ? (
          <Badge variant="outline" className="text-[11px]">
            multiVideo
          </Badge>
        ) : null}
        <Badge
          variant={callBadgeVariant(state.callPhase)}
          className="text-[11px]"
        >
          call: {state.callPhase}
        </Badge>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        <div className="mx-auto max-w-7xl space-y-4">
          {state.lastError ? (
            <div className="flex items-start gap-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
              <RiErrorWarningLine className="mt-0.5 size-4 shrink-0" />
              <span>{state.lastError}</span>
            </div>
          ) : null}

          {state.incomingCall ? (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3">
              <div className="flex items-center gap-2.5 text-sm">
                <RiPhoneFill className="size-4 animate-pulse text-amber-600 dark:text-amber-400" />
                <div>
                  <p className="font-semibold">
                    Incoming call from{' '}
                    <span className="font-mono">{state.incomingCall.from}</span>
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Session{' '}
                    <span className="font-mono">
                      {state.incomingCall.sessionId}
                    </span>
                    {state.incomingCall.to
                      ? ` · To ${state.incomingCall.to}`
                      : ''}
                    {state.incomingCall.hasVideo ? ' · Video' : ' · Audio'}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  size="sm"
                  className="gap-1.5 bg-emerald-600 text-white hover:bg-emerald-700"
                  onClick={() => void acceptIncomingCall()}
                >
                  <RiCheckLine className="size-4" />
                  Accept
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  className="gap-1.5"
                  onClick={rejectIncomingCall}
                >
                  <RiCloseLine className="size-4" />
                  Reject
                </Button>
              </div>
            </div>
          ) : null}

          {/* Row 1: Connection & Call Controls */}
          <div className="grid gap-4 lg:grid-cols-12">
            {/* Connection & Registration Panel */}
            <Card size="sm" className="lg:col-span-7">
              <CardHeader className="border-b border-border/60">
                <div className="flex items-center justify-between gap-2">
                  <div>
                    <CardTitle className="flex items-center gap-1.5">
                      <RiPlugLine className="size-4 text-cyan-600 dark:text-cyan-400" />
                      Connection &amp; Registration
                    </CardTitle>
                    <CardDescription className="text-xs">
                      Open two tabs with different SIP accounts to test camera +
                      screen share end-to-end.
                    </CardDescription>
                  </div>
                  {state.trunkPublicId || state.trunkId ? (
                    <Badge variant="success" className="font-mono text-[10px]">
                      trunk{' '}
                      {state.trunkPublicId
                        ? state.trunkPublicId.slice(0, 8)
                        : `#${state.trunkId}`}
                    </Badge>
                  ) : null}
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid gap-2 sm:grid-cols-12">
                  <div className="space-y-1 sm:col-span-3">
                    <label
                      htmlFor="ms-endpoint"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      Endpoint
                    </label>
                    <Select
                      value={form.endpoint}
                      onValueChange={(value) =>
                        handleEndpointChange(value as GatewayWsEndpoint)
                      }
                      disabled={isConnected || isConnecting}
                    >
                      <SelectTrigger
                        id="ms-endpoint"
                        size="sm"
                        className="w-full font-mono text-xs"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="/ws-agent">/ws-agent</SelectItem>
                        <SelectItem value="/ws">/ws</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-1 sm:col-span-9">
                    <div className="flex items-center justify-between">
                      <label
                        htmlFor="ms-ws-url"
                        className="text-[11px] font-medium text-muted-foreground"
                      >
                        Gateway WebSocket URL
                      </label>
                      <button
                        type="button"
                        className="text-[10px] text-cyan-600 hover:underline disabled:opacity-50 dark:text-cyan-400"
                        disabled={isConnected || isConnecting}
                        onClick={() =>
                          setForm((prev) => ({
                            ...prev,
                            wsUrl: deriveDefaultGatewayWsUrl(prev.endpoint),
                          }))
                        }
                      >
                        Reset default
                      </button>
                    </div>
                    <Input
                      id="ms-ws-url"
                      className="h-8 font-mono text-xs"
                      placeholder="wss://gateway.example.com/ws-agent"
                      value={form.wsUrl}
                      disabled={isConnected || isConnecting}
                      onChange={(e) =>
                        setForm((prev) => ({ ...prev, wsUrl: e.target.value }))
                      }
                    />
                  </div>
                </div>

                <div className="grid gap-2 sm:grid-cols-12">
                  <div className="space-y-1 sm:col-span-4">
                    <label
                      htmlFor="ms-sip-domain"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      SIP Domain
                    </label>
                    <Input
                      id="ms-sip-domain"
                      className="h-8 font-mono text-xs"
                      placeholder="pbx.example.com"
                      value={form.sipDomain}
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          sipDomain: e.target.value,
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1 sm:col-span-3">
                    <label
                      htmlFor="ms-sip-username"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      SIP Username
                    </label>
                    <Input
                      id="ms-sip-username"
                      className="h-8 font-mono text-xs"
                      placeholder="1001"
                      value={form.sipUsername}
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          sipUsername: e.target.value,
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1 sm:col-span-3">
                    <label
                      htmlFor="ms-sip-password"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      SIP Password
                    </label>
                    <Input
                      id="ms-sip-password"
                      type="password"
                      className="h-8 font-mono text-xs"
                      placeholder="••••••"
                      value={form.sipPassword}
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          sipPassword: e.target.value,
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1 sm:col-span-2">
                    <label
                      htmlFor="ms-sip-port"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      Port
                    </label>
                    <Input
                      id="ms-sip-port"
                      className="h-8 font-mono text-xs"
                      placeholder="5060"
                      value={form.sipPort}
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          sipPort: e.target.value,
                        }))
                      }
                    />
                  </div>
                </div>

                <div className="grid gap-2 sm:grid-cols-12">
                  <div className="space-y-1 sm:col-span-6">
                    <label
                      htmlFor="ms-access-token"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      Access Token (optional JWT)
                    </label>
                    <Input
                      id="ms-access-token"
                      type="password"
                      className="h-8 font-mono text-xs"
                      placeholder="Optional Bearer/query access_token"
                      value={form.accessToken}
                      disabled={isConnected || isConnecting}
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          accessToken: e.target.value,
                        }))
                      }
                    />
                  </div>

                  <div className="flex flex-wrap items-end gap-4 pb-1 sm:col-span-6">
                    <label className="flex cursor-pointer items-center gap-2 text-xs">
                      <Checkbox
                        checked={form.multiVideo}
                        onCheckedChange={(checked) =>
                          setForm((prev) => ({
                            ...prev,
                            multiVideo: checked === true,
                          }))
                        }
                      />
                      <span>
                        <code className="font-mono text-[11px]">
                          multiVideo
                        </code>
                      </span>
                    </label>
                    <label className="flex cursor-pointer items-center gap-2 text-xs">
                      <Checkbox
                        checked={form.multiCall}
                        onCheckedChange={(checked) =>
                          setForm((prev) => ({
                            ...prev,
                            multiCall: checked === true,
                          }))
                        }
                      />
                      <span>
                        <code className="font-mono text-[11px]">multiCall</code>
                      </span>
                    </label>
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-2 pt-1">
                  {!isConnected && !isConnecting ? (
                    <Button size="sm" className="gap-1.5" onClick={connect}>
                      <RiLink className="size-3.5" />
                      Connect
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      variant="destructive"
                      className="gap-1.5"
                      onClick={disconnect}
                    >
                      <RiLinkUnlink className="size-3.5" />
                      Disconnect
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    className="gap-1.5"
                    disabled={!isConnected}
                    onClick={registerOrSendCapabilities}
                  >
                    <RiRefreshLine className="size-3.5" />
                    {form.endpoint === '/ws-agent'
                      ? 'Send agent_register'
                      : 'Send client_state'}
                  </Button>
                </div>
              </CardContent>
            </Card>

            {/* Call & Multi-Stream Controls Panel */}
            <Card size="sm" className="lg:col-span-5">
              <CardHeader className="border-b border-border/60">
                <CardTitle className="flex items-center gap-1.5">
                  <RiPhoneLine className="size-4 text-emerald-600 dark:text-emerald-400" />
                  Call &amp; Multi-Stream Controls
                </CardTitle>
                <CardDescription className="text-xs">
                  Dial/answer a call, then toggle screen share on the live
                  PeerConnection.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex items-end gap-2">
                  <div className="flex-1 space-y-1">
                    <label
                      htmlFor="ms-destination"
                      className="text-[11px] font-medium text-muted-foreground"
                    >
                      Dial Destination
                    </label>
                    <Input
                      id="ms-destination"
                      className="h-8 font-mono text-xs"
                      placeholder="1002"
                      value={destination}
                      disabled={!isConnected || isInCall}
                      onChange={(e) => setDestination(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' && isConnected && !isInCall) {
                          void placeCall()
                        }
                      }}
                    />
                  </div>
                  <Button
                    size="sm"
                    className="gap-1.5 bg-emerald-600 text-white hover:bg-emerald-700"
                    disabled={!isConnected || isInCall || !destination.trim()}
                    onClick={() => void placeCall()}
                  >
                    <RiPhoneFill className="size-3.5" />
                    Call
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    className="gap-1.5"
                    disabled={!isInCall}
                    onClick={hangupCall}
                  >
                    <RiShutDownLine className="size-3.5" />
                    Hangup
                  </Button>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    size="sm"
                    variant={previewActive ? 'secondary' : 'outline'}
                    className="gap-1.5 text-xs"
                    onClick={() => void togglePreview()}
                  >
                    {previewActive ? (
                      <RiVideoOnLine className="size-3.5" />
                    ) : (
                      <RiVideoOffLine className="size-3.5" />
                    )}
                    {previewActive ? 'Stop Preview' : 'Camera/Mic Preview'}
                  </Button>

                  <Button
                    size="sm"
                    variant={audioMuted ? 'destructive' : 'outline'}
                    className="gap-1 text-xs"
                    disabled={!previewActive}
                    onClick={toggleAudioMute}
                  >
                    {audioMuted ? (
                      <RiMicOffLine className="size-3.5" />
                    ) : (
                      <RiMicLine className="size-3.5" />
                    )}
                    {audioMuted ? 'Unmute Mic' : 'Mute Mic'}
                  </Button>

                  <Button
                    size="sm"
                    variant={videoMuted ? 'destructive' : 'outline'}
                    className="gap-1 text-xs"
                    disabled={!previewActive}
                    onClick={toggleVideoMute}
                  >
                    {videoMuted ? (
                      <RiVideoOffLine className="size-3.5" />
                    ) : (
                      <RiVideoOnLine className="size-3.5" />
                    )}
                    {videoMuted ? 'Enable Cam' : 'Mute Cam'}
                  </Button>
                </div>

                <div className="border-t border-border/60 pt-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      {!isSharingScreen ? (
                        <Button
                          size="sm"
                          className="gap-1.5 bg-cyan-600 text-white hover:bg-cyan-700"
                          disabled={
                            state.callPhase !== 'active' ||
                            !state.multiVideoEnabled ||
                            state.localScreenStatus === 'stopping'
                          }
                          onClick={() => void startScreenShare()}
                        >
                          <RiShareForwardBoxLine className="size-4" />
                          {state.localScreenTransceiverNegotiated
                            ? 'Re-Share Screen'
                            : 'Share Screen'}
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          variant="destructive"
                          className="gap-1.5"
                          onClick={() => void stopScreenShare()}
                        >
                          <RiStopCircleLine className="size-4" />
                          Stop Share
                        </Button>
                      )}
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge
                        variant="outline"
                        className="font-mono text-[10px]"
                      >
                        tx:{' '}
                        {state.localScreenTransceiverNegotiated
                          ? 'reusable'
                          : 'new'}
                      </Badge>
                      <Badge
                        variant={
                          state.localScreenStatus === 'active'
                            ? 'success'
                            : state.localScreenStatus === 'idle'
                              ? 'secondary'
                              : 'warning'
                        }
                        className="text-[10px]"
                      >
                        send: {state.localScreenStatus}
                      </Badge>
                      <Badge
                        variant={
                          state.remoteScreenStatus === 'active'
                            ? 'success'
                            : state.remoteScreenStatus === 'negotiating'
                              ? 'warning'
                              : 'secondary'
                        }
                        className="text-[10px]"
                      >
                        recv: {state.remoteScreenStatus}
                      </Badge>
                    </div>
                  </div>

                  <div className="mt-2.5 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
                    <span>
                      Session:{' '}
                      <code className="font-mono text-foreground">
                        {state.sessionId ? state.sessionId.slice(0, 14) : '—'}
                      </code>
                    </span>
                    <span>
                      ICE:{' '}
                      <code className="font-mono text-foreground">
                        {iceState}
                      </code>
                    </span>
                    <span>
                      SDP:{' '}
                      <code className="font-mono text-foreground">
                        {signalingState}
                      </code>
                    </span>
                    {state.peerMedia.audioReceiving ? (
                      <Badge
                        variant="success"
                        className="h-4 px-1.5 text-[9px]"
                      >
                        remote audio
                      </Badge>
                    ) : null}
                    {state.peerMedia.videoReceiving ? (
                      <Badge
                        variant="success"
                        className="h-4 px-1.5 text-[9px]"
                      >
                        remote video
                      </Badge>
                    ) : null}
                    {state.endReason ? (
                      <Badge
                        variant="destructive"
                        className="h-4 px-1.5 text-[9px]"
                      >
                        ended: {state.endReason}
                      </Badge>
                    ) : null}
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Multi-Stream Status Banners */}
          {state.localScreenBanner ? (
            <div
              role="status"
              className={`flex items-start gap-2 rounded-md border px-3 py-2 text-xs ${bannerStyle(state.localScreenBanner.tone)}`}
            >
              <BannerIcon tone={state.localScreenBanner.tone} />
              <div>
                <p className="font-medium">{state.localScreenBanner.message}</p>
              </div>
            </div>
          ) : null}

          {state.remoteScreenStatus !== 'idle' || state.remoteScreenReason ? (
            <div
              role="status"
              className={`flex items-start gap-2 rounded-md border px-3 py-2 text-xs ${bannerStyle(
                state.remoteScreenStatus === 'active'
                  ? 'success'
                  : state.remoteScreenStatus === 'negotiating'
                    ? 'info'
                    : 'warning',
              )}`}
            >
              <RiCastLine className="mt-0.5 size-4 shrink-0" />
              <div>
                <p className="font-medium">
                  Remote screen:{' '}
                  <span className="font-mono">{state.remoteScreenStatus}</span>
                  {state.remoteScreenReason
                    ? ` (reason: ${state.remoteScreenReason})`
                    : ''}
                </p>
              </div>
            </div>
          ) : null}

          {/* Row 2: Media Stage (Remote Camera, Remote Screen, Local Camera, Local Screen) */}
          <div className="grid gap-4 md:grid-cols-2">
            <VideoTile
              title="Remote Camera (1st m=video)"
              badge={hasRemoteCamera ? 'receiving' : 'idle'}
              badgeVariant={hasRemoteCamera ? 'success' : 'secondary'}
              videoRef={remoteCameraVideoRef}
              hasStream={hasRemoteCamera}
              emptyLabel="Primary remote camera track will appear here"
            />
            <VideoTile
              title="Remote Screen (2nd m=video / track id: screen)"
              badge={
                state.remoteScreenStatus === 'active'
                  ? 'active'
                  : state.remoteScreenReason
                    ? state.remoteScreenReason
                    : state.remoteScreenStatus
              }
              badgeVariant={
                state.remoteScreenStatus === 'active'
                  ? 'success'
                  : state.remoteScreenStatus === 'negotiating'
                    ? 'warning'
                    : 'secondary'
              }
              videoRef={remoteScreenVideoRef}
              hasStream={
                hasRemoteScreen && state.remoteScreenStatus === 'active'
              }
              highlighted={state.remoteScreenStatus === 'active'}
              emptyLabel={
                state.remoteScreenReason
                  ? `Remote screen closed (${state.remoteScreenReason})`
                  : 'Far-end shared screen will appear here upon renegotiate stream_added'
              }
            />
            <VideoTile
              title="Local Camera"
              badge={previewActive ? (videoMuted ? 'muted' : 'live') : 'off'}
              badgeVariant={
                previewActive
                  ? videoMuted
                    ? 'warning'
                    : 'success'
                  : 'secondary'
              }
              videoRef={localCameraVideoRef}
              hasStream={previewActive}
              emptyLabel="Start Camera/Mic Preview or place/accept a call"
            />
            <VideoTile
              title="Local Screen Capture"
              badge={state.localScreenStatus}
              badgeVariant={
                state.localScreenStatus === 'active'
                  ? 'success'
                  : state.localScreenStatus === 'idle'
                    ? 'secondary'
                    : 'warning'
              }
              videoRef={localScreenVideoRef}
              hasStream={hasLocalScreen}
              highlighted={isSharingScreen}
              emptyLabel="Click Share Screen during an active call"
            />
          </div>

          {/* Dedicated hidden audio playback for remote audio track */}
          <audio ref={remoteAudioRef} autoPlay className="hidden" />

          {/* Row 3: Diagnostics Panel */}
          <Card size="sm">
            <CardHeader className="border-b border-border/60">
              <div className="flex items-center justify-between gap-2">
                <div>
                  <CardTitle className="flex items-center gap-1.5">
                    <RiTerminalBoxLine className="size-4 text-cyan-600 dark:text-cyan-400" />
                    Signaling Diagnostics Log
                  </CardTitle>
                  <CardDescription className="text-xs">
                    Live WebSocket &amp; WebRTC events (passwords and tokens are
                    masked).
                  </CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant="secondary" className="font-mono text-[10px]">
                    {logs.length} events
                  </Badge>
                  <Button
                    size="xs"
                    variant="outline"
                    className="gap-1"
                    disabled={logs.length === 0}
                    onClick={() => setLogs([])}
                  >
                    <RiDeleteBinLine className="size-3" />
                    Clear
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div
                ref={logContainerRef}
                className="h-64 overflow-y-auto rounded-md border border-border/70 bg-zinc-950 p-2 font-mono text-[11px] leading-relaxed text-zinc-200"
              >
                {logs.length === 0 ? (
                  <p className="py-8 text-center text-zinc-500">
                    No signaling events yet. Connect to a gateway endpoint to
                    begin.
                  </p>
                ) : (
                  logs.map((entry) => (
                    <div
                      key={entry.id}
                      className="flex items-baseline gap-2 border-b border-zinc-900/80 py-0.5 last:border-b-0"
                    >
                      <span className="shrink-0 text-zinc-500">
                        {entry.timestamp}
                      </span>
                      <span
                        className={`w-8 shrink-0 font-semibold ${
                          entry.direction === 'IN'
                            ? 'text-emerald-400'
                            : entry.direction === 'OUT'
                              ? 'text-cyan-400'
                              : 'text-amber-400'
                        }`}
                      >
                        {entry.direction}
                      </span>
                      <span className="shrink-0 rounded bg-zinc-800 px-1.5 text-zinc-100">
                        {entry.type}
                      </span>
                      <span className="min-w-0 break-all text-zinc-300">
                        {entry.summary}
                      </span>
                    </div>
                  ))
                )}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
