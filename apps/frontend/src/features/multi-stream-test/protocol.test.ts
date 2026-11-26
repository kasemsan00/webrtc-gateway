import { describe, expect, it } from 'vitest'

import {
  buildAcceptMessage,
  buildAgentRegisterMessage,
  buildCallMessage,
  buildClientStateMessage,
  buildConnectWsUrl,
  buildHangupMessage,
  buildOfferMessage,
  buildRejectMessage,
  buildRenegotiateAnswerMessage,
  buildStreamUpdateAddMessage,
  buildStreamUpdateRemoveMessage,
  countVideoMLines,
  deriveDefaultGatewayWsUrl,
  maskSensitiveUrl,
  parseInboundWsMessage,
  parseSipPort,
  replaceWsUrlEndpoint,
  summarizeWsMessage,
  validateMultiVideoOfferSdp,
  validateStreamAddedOfferSdp,
} from './protocol'

const SINGLE_VIDEO_SDP = [
  'v=0',
  'o=- 1 2 IN IP4 127.0.0.1',
  's=-',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111',
  'a=mid:0',
  'm=video 9 UDP/TLS/RTP/SAVPF 96',
  'a=mid:1',
].join('\r\n')

const DUAL_VIDEO_SDP = [
  'v=0',
  'o=- 1 3 IN IP4 127.0.0.1',
  's=-',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111',
  'a=mid:0',
  'm=video 9 UDP/TLS/RTP/SAVPF 96',
  'a=mid:1',
  'm=video 9 UDP/TLS/RTP/SAVPF 96',
  'a=mid:2',
].join('\r\n')

const TRIPLE_VIDEO_SDP = [
  'v=0',
  'o=- 1 4 IN IP4 127.0.0.1',
  's=-',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111',
  'a=mid:0',
  'm=video 9 UDP/TLS/RTP/SAVPF 96',
  'a=mid:1',
  'm=video 9 UDP/TLS/RTP/SAVPF 96',
  'a=mid:2',
  'm=video 9 UDP/TLS/RTP/SAVPF 96',
  'a=mid:3',
].join('\r\n')

describe('multi-stream protocol helpers', () => {
  it('derives gateway WS URLs from origin or env override and switches endpoints', () => {
    expect(
      deriveDefaultGatewayWsUrl('/ws-agent', {
        origin: 'https://k2-gateway.example.com',
        skipEnv: true,
      }),
    ).toBe('wss://k2-gateway.example.com/ws-agent')

    expect(
      deriveDefaultGatewayWsUrl('/ws', {
        envGatewayUrl: 'http://127.0.0.1:8080/api',
      }),
    ).toBe('ws://127.0.0.1:8080/ws')

    expect(
      replaceWsUrlEndpoint('wss://k2-gateway.example.com/ws-agent', '/ws'),
    ).toBe('wss://k2-gateway.example.com/ws')
  })

  it('appends access_token and masks sensitive URL query params', () => {
    const withToken = buildConnectWsUrl(
      'wss://gw.example.com/ws-agent',
      'secret-jwt-token',
    )
    expect(withToken).toBe(
      'wss://gw.example.com/ws-agent?access_token=secret-jwt-token',
    )
    const masked = maskSensitiveUrl(withToken)
    expect(masked).not.toContain('secret-jwt-token')
    expect(masked).toContain('access_token=***')
  })

  it('builds agent_register and client_state with multiVideo opt-in', () => {
    expect(
      buildAgentRegisterMessage({
        sipDomain: ' pbx.local ',
        sipUsername: ' 1001 ',
        sipPassword: 'pw',
        sipPort: 5060,
        multiVideo: true,
        multiCall: true,
      }),
    ).toEqual({
      type: 'agent_register',
      sipDomain: 'pbx.local',
      sipUsername: '1001',
      sipPassword: 'pw',
      sipPort: 5060,
      multiCall: true,
      multiVideo: true,
    })

    expect(
      buildClientStateMessage({
        availability: 'idle',
        multiVideo: true,
      }),
    ).toEqual({
      type: 'client_state',
      availability: 'idle',
      multiVideo: true,
    })
  })

  it('builds stream_update add (with and without SDP), remove, and renegotiate_answer', () => {
    expect(buildStreamUpdateAddMessage('sess-1', DUAL_VIDEO_SDP)).toEqual({
      type: 'stream_update',
      sessionId: 'sess-1',
      role: 'screen',
      action: 'add',
      sdp: DUAL_VIDEO_SDP,
    })

    const reuseMsg = buildStreamUpdateAddMessage('sess-1')
    expect(reuseMsg).toEqual({
      type: 'stream_update',
      sessionId: 'sess-1',
      role: 'screen',
      action: 'add',
    })
    expect('sdp' in reuseMsg).toBe(false)

    expect(buildStreamUpdateRemoveMessage('sess-1')).toEqual({
      type: 'stream_update',
      sessionId: 'sess-1',
      role: 'screen',
      action: 'remove',
    })

    expect(
      buildRenegotiateAnswerMessage({
        sessionId: 'sess-1',
        renegotiationId: 'ren-42',
        status: 'ok',
        sdp: DUAL_VIDEO_SDP,
      }),
    ).toEqual({
      type: 'renegotiate_answer',
      sessionId: 'sess-1',
      renegotiationId: 'ren-42',
      status: 'ok',
      sdp: DUAL_VIDEO_SDP,
    })
  })

  it('builds call control messages cleanly', () => {
    expect(buildOfferMessage(SINGLE_VIDEO_SDP, 'sess-in')).toEqual({
      type: 'offer',
      sdp: SINGLE_VIDEO_SDP,
      sessionId: 'sess-in',
    })
    expect(
      buildCallMessage({
        sessionId: 'sess-1',
        destination: ' 1002 ',
      }),
    ).toEqual({
      type: 'call',
      sessionId: 'sess-1',
      destination: '1002',
    })
    expect(buildAcceptMessage('sess-in')).toEqual({
      type: 'accept',
      sessionId: 'sess-in',
    })
    expect(buildRejectMessage('sess-in')).toEqual({
      type: 'reject',
      sessionId: 'sess-in',
      reason: 'busy',
    })
    expect(buildHangupMessage('sess-1', 'user_hangup')).toEqual({
      type: 'hangup',
      sessionId: 'sess-1',
      reason: 'user_hangup',
    })
    expect(parseSipPort('5060')).toBe(5060)
    expect(parseSipPort('invalid')).toBeUndefined()
  })

  it('counts and validates m=video sections for multi-video SDP offers', () => {
    expect(countVideoMLines(SINGLE_VIDEO_SDP)).toBe(1)
    expect(countVideoMLines(DUAL_VIDEO_SDP)).toBe(2)

    expect(validateMultiVideoOfferSdp(SINGLE_VIDEO_SDP)).toMatchObject({
      valid: false,
      videoCount: 1,
      audioCount: 1,
      videoMids: ['1'],
    })

    expect(validateMultiVideoOfferSdp(DUAL_VIDEO_SDP)).toEqual({
      valid: true,
      videoCount: 2,
      audioCount: 1,
      videoMids: ['1', '2'],
    })

    // First stream_update add strictly requires exactly 2 video m-lines
    expect(validateMultiVideoOfferSdp(TRIPLE_VIDEO_SDP)).toMatchObject({
      valid: false,
      videoCount: 3,
    })

    // Inbound renegotiate stream_added allows 2 or 3+ video m-lines (when a kept sendonly screen transceiver exists)
    expect(validateStreamAddedOfferSdp(SINGLE_VIDEO_SDP, 2)).toMatchObject({
      valid: false,
      videoCount: 1,
    })
    expect(validateStreamAddedOfferSdp(DUAL_VIDEO_SDP, 2)).toEqual({
      valid: true,
      videoCount: 2,
      audioCount: 1,
      videoMids: ['1', '2'],
    })
    expect(validateStreamAddedOfferSdp(DUAL_VIDEO_SDP, 3)).toMatchObject({
      valid: false,
      videoCount: 2,
    })
    expect(validateStreamAddedOfferSdp(TRIPLE_VIDEO_SDP, 3)).toEqual({
      valid: true,
      videoCount: 3,
      audioCount: 1,
      videoMids: ['1', '2', '3'],
    })
  })

  it('parses valid inbound WS payloads and rejects malformed input', () => {
    expect(parseInboundWsMessage('not-json')).toBeNull()
    expect(parseInboundWsMessage('{}')).toBeNull()

    const parsed = parseInboundWsMessage(
      JSON.stringify({
        type: 'stream_state',
        sessionId: 'sess-1',
        role: 'screen',
        active: false,
        reason: 'peer_not_supported',
      }),
    )
    expect(parsed).toMatchObject({
      type: 'stream_state',
      sessionId: 'sess-1',
      role: 'screen',
      active: false,
      reason: 'peer_not_supported',
    })
  })

  it('masks SIP passwords when summarizing WS messages for diagnostics', () => {
    const regSummary = summarizeWsMessage(
      buildAgentRegisterMessage({
        sipDomain: 'pbx.local',
        sipUsername: '1001',
        sipPassword: 'super-secret-password',
        multiVideo: true,
      }),
    )
    expect(regSummary).not.toContain('super-secret-password')
    expect(regSummary).toContain('password=***')
    expect(regSummary).toContain('multiVideo=true')

    const callSummary = summarizeWsMessage(
      buildCallMessage({
        sessionId: 'sess-1',
        destination: '1002',
        sipCredentials: {
          sipDomain: 'pbx.local',
          sipUsername: '1001',
          sipPassword: 'another-secret-password',
        },
      }),
    )
    expect(callSummary).not.toContain('another-secret-password')
    expect(callSummary).toContain('sipPassword=***')

    const streamSummary = summarizeWsMessage(
      buildStreamUpdateAddMessage('sess-1', DUAL_VIDEO_SDP),
    )
    expect(streamSummary).toContain('action=add')
    expect(streamSummary).toContain('m=video:2')
  })
})
