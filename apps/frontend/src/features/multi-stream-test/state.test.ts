import { describe, expect, it } from 'vitest'

import {
  INITIAL_MULTI_STREAM_STATE,
  classifyRemoteVideoTrack,
  isLocalScreenSharingOrPending,
  multiStreamReducer,
} from './state'
import type { MultiStreamState } from './types'

function activeCallState(): MultiStreamState {
  let state = multiStreamReducer(INITIAL_MULTI_STREAM_STATE, {
    type: 'WS_CONNECTING',
    multiVideo: true,
    multiCall: false,
  })
  state = multiStreamReducer(state, { type: 'WS_OPEN' })
  state = multiStreamReducer(state, {
    type: 'INBOUND_WS',
    message: { type: 'trunk_resolved', trunkId: 7, trunkPublicId: 'pub-7' },
  })
  state = multiStreamReducer(state, {
    type: 'INBOUND_WS',
    message: { type: 'answer', sessionId: 'sess-1', sdp: 'v=0' },
  })
  state = multiStreamReducer(state, {
    type: 'INBOUND_WS',
    message: { type: 'state', sessionId: 'sess-1', state: 'active' },
  })
  return state
}

describe('multiStreamReducer', () => {
  it('handles first screen share, confirmation, stop (keeping transceiver), and re-share without SDP', () => {
    let state = activeCallState()
    expect(state.callPhase).toBe('active')
    expect(state.localScreenTransceiverNegotiated).toBe(false)

    // 1. First share with SDP
    state = multiStreamReducer(state, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: false,
    })
    expect(state.localScreenStatus).toBe('negotiating')
    expect(state.pendingStreamAction).toBe('add_with_sdp')

    // 2. Gateway returns stream_update_result ok with SDP
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'ok',
        sdp: 'v=0\r\nm=video 9\r\nm=video 9',
      },
    })
    expect(state.localScreenTransceiverNegotiated).toBe(true)
    expect(state.localScreenStatus).toBe('waiting_peer')

    // 3. Far end confirms via stream_state active:true
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: true,
      },
    })
    expect(state.localScreenStatus).toBe('active')
    expect(state.localScreenConfirmedByPeer).toBe(true)

    // 4. Stop share -> transceiver remains negotiated for reuse
    state = multiStreamReducer(state, { type: 'LOCAL_SCREEN_STOP_START' })
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'remove',
        status: 'ok',
      },
    })
    expect(state.localScreenStatus).toBe('idle')
    expect(state.localScreenTransceiverNegotiated).toBe(true)
    expect(state.callPhase).toBe('active')

    // 5. Re-share reuses transceiver (no SDP)
    state = multiStreamReducer(state, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: true,
    })
    expect(state.pendingStreamAction).toBe('add_reuse')
  })

  it('handles stream_state active:true arriving before stream_update_result ok', () => {
    let state = activeCallState()
    state = multiStreamReducer(state, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: false,
    })

    // Far-end confirmation arrives slightly ahead of stream_update_result
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: true,
      },
    })
    expect(state.localScreenConfirmedByPeer).toBe(true)

    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'ok',
        sdp: 'v=0',
      },
    })
    expect(state.localScreenStatus).toBe('active')
    expect(state.localScreenTransceiverNegotiated).toBe(true)
  })

  it('handles stream_update_result rejected (not_supported) and failed without breaking active call', () => {
    let state = activeCallState()
    state = multiStreamReducer(state, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: false,
    })

    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'rejected',
        reason: 'not_supported',
      },
    })
    expect(state.callPhase).toBe('active')
    expect(state.localScreenStatus).toBe('idle')
    expect(state.localScreenTransceiverNegotiated).toBe(false)
    expect(state.localScreenBanner?.tone).toBe('warning')
    expect(state.localScreenBanner?.reason).toBe('not_supported')

    // Also handle failed reason (e.g. peer_sharing)
    state = multiStreamReducer(state, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: false,
    })
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'failed',
        reason: 'peer_sharing',
      },
    })
    expect(state.callPhase).toBe('active')
    expect(state.localScreenStatus).toBe('idle')
    expect(state.localScreenBanner?.reason).toBe('peer_sharing')
  })

  it('handles stream_state active:false reasons (peer_not_supported, peer_closed, agent_switch, resume)', () => {
    // peer_not_supported on sender side
    let sender = activeCallState()
    sender = multiStreamReducer(sender, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: false,
    })
    sender = multiStreamReducer(sender, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'ok',
        sdp: 'v=0',
      },
    })
    sender = multiStreamReducer(sender, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: false,
        reason: 'peer_not_supported',
      },
    })
    expect(sender.localScreenStatus).toBe('idle')
    expect(sender.localScreenBanner?.reason).toBe('peer_not_supported')
    expect(sender.localScreenTransceiverNegotiated).toBe(true)

    // agent_switch resets negotiated transceiver flags
    sender = multiStreamReducer(sender, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: false,
        reason: 'agent_switch',
      },
    })
    expect(sender.localScreenTransceiverNegotiated).toBe(false)
    expect(sender.localScreenBanner?.reason).toBe('agent_switch')

    // resume also resets negotiated transceiver flags
    let resuming = activeCallState()
    resuming = {
      ...resuming,
      localScreenStatus: 'active',
      localScreenTransceiverNegotiated: true,
    }
    resuming = multiStreamReducer(resuming, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: false,
        reason: 'resume',
      },
    })
    expect(resuming.localScreenStatus).toBe('idle')
    expect(resuming.localScreenTransceiverNegotiated).toBe(false)
    expect(resuming.localScreenBanner?.reason).toBe('resume')
  })

  it('handles inbound renegotiate stream_added -> renegotiate_result ok, peer_closed, and re-share', () => {
    let state = activeCallState()

    // Receive renegotiate stream_added
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'renegotiate',
        sessionId: 'sess-1',
        renegotiationId: 'ren-1',
        reason: 'stream_added',
        role: 'screen',
        mediaDirection: 'recvonly',
        hasVideo: 'true',
        requiresAnswer: true,
        status: 'pending',
      },
    })
    expect(state.remoteScreenStatus).toBe('negotiating')
    expect(state.pendingRenegotiation?.renegotiationId).toBe('ren-1')

    // Gateway returns renegotiate_result ok
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'renegotiate_result',
        sessionId: 'sess-1',
        renegotiationId: 'ren-1',
        status: 'ok',
        sdp: 'v=0',
      },
    })
    expect(state.remoteScreenStatus).toBe('active')
    expect(state.remoteScreenTransceiverNegotiated).toBe(true)
    expect(state.pendingRenegotiation).toBeNull()

    // Peer closes screen share
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: false,
        reason: 'peer_closed',
      },
    })
    expect(state.remoteScreenStatus).toBe('closed')
    expect(state.remoteScreenReason).toBe('peer_closed')
    expect(state.remoteScreenTransceiverNegotiated).toBe(true)

    // Peer re-shares without renegotiation -> stream_state active:true
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: true,
      },
    })
    expect(state.remoteScreenStatus).toBe('active')
    expect(state.remoteScreenReason).toBeNull()
  })

  it('handles renegotiate_result timeout/failed cleanly', () => {
    let state = activeCallState()
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'renegotiate',
        sessionId: 'sess-1',
        renegotiationId: 'ren-2',
        reason: 'stream_added',
        role: 'screen',
      },
    })
    state = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'renegotiate_result',
        sessionId: 'sess-1',
        renegotiationId: 'ren-2',
        status: 'timeout',
        reason: 'timeout',
      },
    })
    expect(state.remoteScreenStatus).toBe('idle')
    expect(state.remoteScreenReason).toBe('timeout')
    expect(state.pendingRenegotiation).toBeNull()
  })

  it('handles stopScreenShare while add_with_sdp is in flight without breaking state', () => {
    let state = activeCallState()
    state = multiStreamReducer(state, {
      type: 'LOCAL_SCREEN_START',
      reusedTransceiver: false,
    })
    state = multiStreamReducer(state, { type: 'LOCAL_SCREEN_STOP_START' })
    expect(state.localScreenStatus).toBe('stopping')

    // Case A: in-flight add rejected -> returns cleanly to idle with warning banner
    const rejected = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'rejected',
        reason: 'not_supported',
      },
    })
    expect(rejected.localScreenStatus).toBe('idle')
    expect(rejected.localScreenTransceiverNegotiated).toBe(false)
    expect(rejected.localScreenBanner?.reason).toBe('not_supported')

    // Case B: in-flight add succeeded -> marks transceiver negotiated while staying in stopping until remove finishes
    const accepted = multiStreamReducer(state, {
      type: 'INBOUND_WS',
      message: {
        type: 'stream_update_result',
        sessionId: 'sess-1',
        role: 'screen',
        action: 'add',
        status: 'ok',
        sdp: 'v=0',
      },
    })
    expect(accepted.localScreenStatus).toBe('stopping')
    expect(accepted.localScreenTransceiverNegotiated).toBe(true)
  })

  it('identifies local sharing/pending statuses accurately for stream_state disambiguation', () => {
    expect(isLocalScreenSharingOrPending('negotiating')).toBe(true)
    expect(isLocalScreenSharingOrPending('waiting_peer')).toBe(true)
    expect(isLocalScreenSharingOrPending('active')).toBe(true)
    expect(isLocalScreenSharingOrPending('idle')).toBe(false)
    expect(isLocalScreenSharingOrPending('stopping')).toBe(false)
  })
})

describe('classifyRemoteVideoTrack', () => {
  it('identifies primary camera track on first video transceiver', () => {
    expect(
      classifyRemoteVideoTrack({
        trackId: 'video-camera-0',
        transceiverMid: '1',
        screenRecvMid: null,
        isKnownScreenTransceiver: false,
        videoTransceiverIndex: 0,
        hasCameraVideoTrack: false,
        cameraTransceiverMid: null,
      }),
    ).toBe('camera')
  })

  it('identifies screen track by track id "screen", transceiver match, or 2nd video transceiver index', () => {
    expect(
      classifyRemoteVideoTrack({
        trackId: 'screen',
        transceiverMid: '2',
        screenRecvMid: null,
        isKnownScreenTransceiver: false,
        videoTransceiverIndex: 0,
        hasCameraVideoTrack: false,
        cameraTransceiverMid: null,
      }),
    ).toBe('screen')

    expect(
      classifyRemoteVideoTrack({
        trackId: 'remote-v2',
        transceiverMid: '2',
        screenRecvMid: '2',
        isKnownScreenTransceiver: false,
        videoTransceiverIndex: 1,
        hasCameraVideoTrack: true,
        cameraTransceiverMid: '1',
      }),
    ).toBe('screen')

    expect(
      classifyRemoteVideoTrack({
        trackId: 'remote-v2',
        transceiverMid: null,
        screenRecvMid: null,
        isKnownScreenTransceiver: true,
        videoTransceiverIndex: 0,
        hasCameraVideoTrack: true,
        cameraTransceiverMid: '1',
      }),
    ).toBe('screen')
  })
})
