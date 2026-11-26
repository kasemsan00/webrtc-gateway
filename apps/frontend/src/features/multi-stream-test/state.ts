import type {
  InboundWsMessage,
  LocalScreenStatus,
  MultiStreamState,
  StatusBanner,
} from './types'

export function isLocalScreenSharingOrPending(
  status: LocalScreenStatus,
): boolean {
  return (
    status === 'negotiating' || status === 'waiting_peer' || status === 'active'
  )
}

export const INITIAL_MULTI_STREAM_STATE: MultiStreamState = {
  connectionStatus: 'disconnected',
  multiVideoEnabled: true,
  multiCallEnabled: false,
  trunkId: null,
  trunkPublicId: null,
  lastError: null,

  callPhase: 'idle',
  sessionId: null,
  incomingCall: null,
  endReason: null,
  peerMedia: { audioReceiving: false, videoReceiving: false },

  localScreenStatus: 'idle',
  localScreenTransceiverNegotiated: false,
  localScreenConfirmedByPeer: false,
  pendingStreamAction: null,
  localScreenBanner: null,

  remoteScreenStatus: 'idle',
  remoteScreenTransceiverNegotiated: false,
  remoteScreenReason: null,
  pendingRenegotiation: null,
}

export type MultiStreamAction =
  | {
      type: 'WS_CONNECTING'
      multiVideo: boolean
      multiCall: boolean
    }
  | { type: 'WS_OPEN' }
  | { type: 'LOCAL_CAPABILITIES_SENT'; multiVideo: boolean; multiCall: boolean }
  | { type: 'WS_CLOSED'; reason?: string }
  | { type: 'WS_ERROR'; error: string }
  | { type: 'LOCAL_CALL_START' }
  | { type: 'LOCAL_INCOMING_ACCEPT_START'; sessionId: string }
  | { type: 'LOCAL_CALL_RESET' }
  | { type: 'LOCAL_SCREEN_START'; reusedTransceiver: boolean }
  | { type: 'LOCAL_SCREEN_LOCAL_ERROR'; message: string; reason?: string }
  | { type: 'LOCAL_SCREEN_STOP_START' }
  | { type: 'LOCAL_SCREEN_STOP_COMPLETE' }
  | { type: 'INBOUND_WS'; message: InboundWsMessage }

function resetCallFields(
  state: MultiStreamState,
  overrides: Partial<MultiStreamState> = {},
): MultiStreamState {
  return {
    ...state,
    callPhase: 'idle',
    sessionId: null,
    incomingCall: null,
    endReason: null,
    peerMedia: { audioReceiving: false, videoReceiving: false },
    localScreenStatus: 'idle',
    localScreenTransceiverNegotiated: false,
    localScreenConfirmedByPeer: false,
    pendingStreamAction: null,
    localScreenBanner: null,
    remoteScreenStatus: 'idle',
    remoteScreenTransceiverNegotiated: false,
    remoteScreenReason: null,
    pendingRenegotiation: null,
    ...overrides,
  }
}

function describeStreamUpdateFailure(
  status: string,
  reason: string | undefined,
): StatusBanner {
  if (status === 'rejected' && reason === 'not_supported') {
    return {
      tone: 'warning',
      reason,
      message:
        'Screen share rejected (not_supported): SIP peer or endpoint does not accept a second video stream. Camera and audio remain active.',
    }
  }
  if (reason === 'peer_sharing') {
    return {
      tone: 'warning',
      reason,
      message:
        'Cannot share screen while the far end is already sharing (peer_sharing).',
    }
  }
  if (reason === 'busy') {
    return {
      tone: 'warning',
      reason,
      message:
        'Another mid-call negotiation is in progress (busy). Try again shortly.',
    }
  }
  if (reason === 'multi_video_not_enabled') {
    return {
      tone: 'warning',
      reason,
      message:
        'Connection did not opt in to multiVideo. Enable multiVideo and reconnect/register.',
    }
  }
  const detail = reason ? ` (${reason})` : ''
  return {
    tone: status === 'rejected' ? 'warning' : 'error',
    reason,
    message: `Screen share ${status}${detail}. Camera and audio remain active.`,
  }
}

function describeStreamStateClosed(reason: string | undefined): StatusBanner {
  switch (reason) {
    case 'peer_not_supported':
      return {
        tone: 'warning',
        reason,
        message:
          'Screen share stopped (peer_not_supported): far end refused or did not confirm within 3s. Local capture stopped.',
      }
    case 'agent_switch':
      return {
        tone: 'info',
        reason,
        message:
          'Screen share ended due to agent switch (agent_switch). You can share again once switched.',
      }
    case 'resume':
      return {
        tone: 'info',
        reason,
        message:
          'Screen share ended during session resume (resume). Start a new share to send an offer.',
      }
    case 'peer_closed':
      return {
        tone: 'info',
        reason,
        message: 'Far end stopped sharing its screen (peer_closed).',
      }
    default:
      return {
        tone: 'info',
        reason,
        message: reason
          ? `Screen stream closed (${reason}).`
          : 'Screen stream closed.',
      }
  }
}

function handleInboundWsMessage(
  state: MultiStreamState,
  msg: InboundWsMessage,
): MultiStreamState {
  switch (msg.type) {
    case 'trunk_resolved':
      return {
        ...state,
        connectionStatus: 'registered',
        trunkId: msg.trunkId ?? state.trunkId,
        trunkPublicId: msg.trunkPublicId ?? state.trunkPublicId,
        lastError: null,
      }

    case 'answer':
      return {
        ...state,
        sessionId: msg.sessionId ?? state.sessionId,
        callPhase:
          state.callPhase === 'idle' || state.callPhase === 'ended'
            ? 'dialing'
            : state.callPhase,
      }

    case 'incoming': {
      if (!msg.sessionId) return state
      return {
        ...state,
        callPhase: state.callPhase === 'active' ? state.callPhase : 'incoming',
        incomingCall: {
          sessionId: msg.sessionId,
          from: msg.from ?? 'unknown',
          to: msg.to ?? '',
          hasVideo: msg.hasVideo !== 'false',
        },
        endReason: null,
      }
    }

    case 'cancel': {
      if (
        state.incomingCall &&
        msg.sessionId === state.incomingCall.sessionId
      ) {
        return {
          ...state,
          incomingCall: null,
          callPhase: state.callPhase === 'incoming' ? 'idle' : state.callPhase,
          endReason: msg.reason ?? 'cancelled',
        }
      }
      return state
    }

    case 'ringing':
      return {
        ...state,
        sessionId: msg.sessionId ?? state.sessionId,
        callPhase: state.callPhase === 'active' ? 'active' : 'ringing',
      }

    case 'state': {
      const nextSessionId = msg.sessionId ?? state.sessionId
      if (msg.state === 'connecting') {
        return {
          ...state,
          sessionId: nextSessionId,
          callPhase: state.callPhase === 'active' ? 'active' : 'dialing',
        }
      }
      if (msg.state === 'ringing') {
        return {
          ...state,
          sessionId: nextSessionId,
          callPhase: state.callPhase === 'active' ? 'active' : 'ringing',
        }
      }
      if (msg.state === 'active') {
        return {
          ...state,
          sessionId: nextSessionId,
          incomingCall:
            state.incomingCall?.sessionId === nextSessionId
              ? null
              : state.incomingCall,
          callPhase: 'active',
          endReason: null,
        }
      }
      if (msg.state === 'ended') {
        if (
          state.sessionId &&
          msg.sessionId &&
          msg.sessionId !== state.sessionId
        ) {
          return state
        }
        return resetCallFields(state, {
          callPhase: 'ended',
          endReason: msg.endReason ?? msg.reason ?? null,
        })
      }
      return state
    }

    case 'media': {
      if (msg.direction === 'remote' && msg.state === 'receiving') {
        return {
          ...state,
          peerMedia: {
            audioReceiving:
              msg.kind === 'audio' ? true : state.peerMedia.audioReceiving,
            videoReceiving:
              msg.kind === 'video' ? true : state.peerMedia.videoReceiving,
          },
        }
      }
      return state
    }

    case 'stream_update_result': {
      if (msg.role && msg.role !== 'screen') return state
      const action = msg.action
      const status = msg.status ?? 'failed'

      if (action === 'add') {
        if (status === 'ok') {
          const negotiated =
            state.localScreenTransceiverNegotiated ||
            state.pendingStreamAction === 'add_with_sdp' ||
            Boolean(msg.sdp)
          if (state.localScreenStatus === 'stopping') {
            return {
              ...state,
              localScreenTransceiverNegotiated: negotiated,
            }
          }
          const confirmed = state.localScreenConfirmedByPeer
          return {
            ...state,
            localScreenTransceiverNegotiated: negotiated,
            pendingStreamAction: null,
            localScreenStatus: confirmed ? 'active' : 'waiting_peer',
            localScreenBanner: confirmed
              ? {
                  tone: 'success',
                  message: 'Screen share active and confirmed by far end.',
                }
              : {
                  tone: 'info',
                  message:
                    'SIP peer accepted screen share; waiting for far-end confirmation…',
                },
          }
        }
        return {
          ...state,
          localScreenStatus: 'idle',
          localScreenConfirmedByPeer: false,
          pendingStreamAction: null,
          localScreenBanner: describeStreamUpdateFailure(status, msg.reason),
        }
      }

      if (action === 'remove') {
        return {
          ...state,
          localScreenStatus: 'idle',
          localScreenConfirmedByPeer: false,
          pendingStreamAction: null,
          localScreenBanner:
            status === 'ok'
              ? {
                  tone: 'info',
                  message:
                    'Screen share stopped (transceiver kept for fast re-share).',
                }
              : describeStreamUpdateFailure(status, msg.reason),
        }
      }

      return state
    }

    case 'stream_state': {
      if (msg.role && msg.role !== 'screen') return state
      const active = Boolean(msg.active)
      const reason = msg.reason
      const isLocallySharingOrPending = isLocalScreenSharingOrPending(
        state.localScreenStatus,
      )

      if (active) {
        if (isLocallySharingOrPending) {
          return {
            ...state,
            localScreenConfirmedByPeer: true,
            localScreenStatus:
              state.localScreenStatus === 'waiting_peer'
                ? 'active'
                : state.localScreenStatus,
            localScreenBanner: {
              tone: 'success',
              message: 'Screen share active and confirmed by far end.',
            },
          }
        }
        // Receiver side: far end started/resumed sharing on existing screen transceiver
        return {
          ...state,
          remoteScreenStatus: 'active',
          remoteScreenTransceiverNegotiated: true,
          remoteScreenReason: null,
        }
      }

      // active === false
      const resetsTransceivers =
        reason === 'agent_switch' || reason === 'resume'

      let nextState = state
      if (
        isLocallySharingOrPending ||
        reason === 'peer_not_supported' ||
        resetsTransceivers
      ) {
        nextState = {
          ...nextState,
          localScreenStatus: 'idle',
          localScreenConfirmedByPeer: false,
          pendingStreamAction: null,
          localScreenTransceiverNegotiated: resetsTransceivers
            ? false
            : nextState.localScreenTransceiverNegotiated,
          localScreenBanner: describeStreamStateClosed(reason),
        }
      }

      if (
        nextState.remoteScreenStatus === 'active' ||
        nextState.remoteScreenStatus === 'negotiating' ||
        reason === 'peer_closed' ||
        resetsTransceivers
      ) {
        nextState = {
          ...nextState,
          remoteScreenStatus: resetsTransceivers ? 'idle' : 'closed',
          remoteScreenTransceiverNegotiated: resetsTransceivers
            ? false
            : nextState.remoteScreenTransceiverNegotiated,
          remoteScreenReason: reason ?? 'peer_closed',
        }
      }

      return nextState
    }

    case 'renegotiate': {
      if (
        msg.reason === 'stream_added' &&
        (!msg.role || msg.role === 'screen') &&
        msg.renegotiationId &&
        msg.sessionId
      ) {
        return {
          ...state,
          remoteScreenStatus: 'negotiating',
          remoteScreenReason: null,
          pendingRenegotiation: {
            renegotiationId: msg.renegotiationId,
            sessionId: msg.sessionId,
            reason: msg.reason,
            role: msg.role ?? 'screen',
            mediaDirection: msg.mediaDirection,
          },
        }
      }
      return state
    }

    case 'renegotiate_result': {
      if (
        !state.pendingRenegotiation ||
        (msg.renegotiationId &&
          msg.renegotiationId !== state.pendingRenegotiation.renegotiationId)
      ) {
        return state
      }

      if (msg.status === 'ok') {
        return {
          ...state,
          remoteScreenStatus: 'active',
          remoteScreenTransceiverNegotiated: true,
          remoteScreenReason: null,
          pendingRenegotiation: null,
        }
      }

      return {
        ...state,
        remoteScreenStatus: 'idle',
        remoteScreenReason: msg.reason ?? msg.status ?? 'failed',
        pendingRenegotiation: null,
      }
    }

    case 'error':
      return {
        ...state,
        lastError: msg.error ?? 'Gateway error',
      }

    default:
      return state
  }
}

export function multiStreamReducer(
  state: MultiStreamState,
  action: MultiStreamAction,
): MultiStreamState {
  switch (action.type) {
    case 'WS_CONNECTING':
      return {
        ...resetCallFields(state),
        connectionStatus: 'connecting',
        multiVideoEnabled: action.multiVideo,
        multiCallEnabled: action.multiCall,
        trunkId: null,
        trunkPublicId: null,
        lastError: null,
      }

    case 'WS_OPEN':
      return {
        ...state,
        connectionStatus: 'connected',
        lastError: null,
      }

    case 'LOCAL_CAPABILITIES_SENT':
      return {
        ...state,
        multiVideoEnabled: state.multiVideoEnabled || action.multiVideo,
        multiCallEnabled: state.multiCallEnabled || action.multiCall,
      }

    case 'WS_CLOSED':
      return {
        ...resetCallFields(state),
        connectionStatus: 'disconnected',
        trunkId: null,
        trunkPublicId: null,
        lastError: action.reason
          ? `Disconnected: ${action.reason}`
          : state.lastError,
      }

    case 'WS_ERROR':
      return {
        ...state,
        lastError: action.error,
      }

    case 'LOCAL_CALL_START':
      return {
        ...resetCallFields(state),
        callPhase: 'dialing',
        lastError: null,
      }

    case 'LOCAL_INCOMING_ACCEPT_START':
      return {
        ...resetCallFields(state, {
          callPhase: 'dialing',
          sessionId: action.sessionId,
          incomingCall: state.incomingCall,
        }),
        lastError: null,
      }

    case 'LOCAL_CALL_RESET':
      return resetCallFields(state)

    case 'LOCAL_SCREEN_START':
      return {
        ...state,
        localScreenStatus: 'negotiating',
        localScreenConfirmedByPeer: false,
        pendingStreamAction: action.reusedTransceiver
          ? 'add_reuse'
          : 'add_with_sdp',
        localScreenBanner: {
          tone: 'info',
          message: action.reusedTransceiver
            ? 'Re-sharing screen on existing transceiver (no SDP renegotiation)…'
            : 'Negotiating 2nd video transceiver (screen) on live PeerConnection…',
        },
      }

    case 'LOCAL_SCREEN_LOCAL_ERROR':
      return {
        ...state,
        localScreenStatus: 'idle',
        localScreenConfirmedByPeer: false,
        pendingStreamAction: null,
        localScreenBanner: {
          tone: 'error',
          reason: action.reason,
          message: action.message,
        },
      }

    case 'LOCAL_SCREEN_STOP_START':
      return {
        ...state,
        localScreenStatus: 'stopping',
        localScreenConfirmedByPeer: false,
        pendingStreamAction: 'remove',
      }

    case 'LOCAL_SCREEN_STOP_COMPLETE':
      return {
        ...state,
        localScreenStatus: 'idle',
        localScreenConfirmedByPeer: false,
        pendingStreamAction: null,
        localScreenBanner: {
          tone: 'info',
          message: 'Screen share stopped.',
        },
      }

    case 'INBOUND_WS':
      return handleInboundWsMessage(state, action.message)
  }
}

export interface ClassifyRemoteVideoTrackInput {
  trackId: string
  transceiverMid: string | null
  screenRecvMid: string | null
  isKnownScreenTransceiver: boolean
  videoTransceiverIndex: number
  hasCameraVideoTrack: boolean
  cameraTransceiverMid: string | null
}

/**
 * Classifies an incoming remote video track as primary `'camera'` or secondary
 * `'screen'`, following `docs/gateway/ws-contract.md` (track id `screen` or 2nd
 * video transceiver).
 */
export function classifyRemoteVideoTrack(
  input: ClassifyRemoteVideoTrackInput,
): 'camera' | 'screen' {
  if (input.trackId.toLowerCase() === 'screen') return 'screen'
  if (input.isKnownScreenTransceiver) return 'screen'
  if (
    input.screenRecvMid &&
    input.transceiverMid &&
    input.transceiverMid === input.screenRecvMid
  ) {
    return 'screen'
  }
  if (input.videoTransceiverIndex >= 1) return 'screen'
  if (
    input.hasCameraVideoTrack &&
    input.cameraTransceiverMid &&
    input.transceiverMid &&
    input.transceiverMid !== input.cameraTransceiverMid
  ) {
    return 'screen'
  }
  return 'camera'
}
