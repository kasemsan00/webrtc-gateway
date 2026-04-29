import { describe, expect, it } from 'vitest'

import {
  canPlaceCall,
  canResolveTrunk,
  isCallInProgress,
  normalizeCallStatus,
} from './gateway-store'
import type { GatewayState } from '../types'

function createState(overrides?: Partial<GatewayState>): GatewayState {
  return {
    config: { wssUrl: 'ws://localhost:8000/ws' },
    connection: { status: 'connected', wsStateText: 'Connected' },
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
      translatorEnabled: false,
      translatorSrcLang: '',
      translatorTgtLang: '',
    },
    mode: 'public',
    publicCredentials: {
      sipDomain: 'example.com',
      sipUsername: '1001',
      sipPassword: 'secret',
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
    ...overrides,
  }
}

describe('canPlaceCall', () => {
  it('returns true when websocket and credentials are ready', () => {
    expect(canPlaceCall(createState())).toBe(true)
  })

  it('returns false while call is in progress', () => {
    expect(
      canPlaceCall(
        createState({
          call: {
            sessionId: 's1',
            state: 'active',
            elapsedSeconds: 1,
            callCount: 1,
            translatorEnabled: false,
            translatorSrcLang: '',
            translatorTgtLang: '',
          },
        }),
      ),
    ).toBe(false)
  })

  it('returns false when auto-start session is running', () => {
    expect(
      canPlaceCall(
        createState({
          controls: {
            destination: '9999',
            dialpadOpen: false,
            statsOpen: false,
            isMutedAudio: false,
            isMutedVideo: false,
            autoStartingSession: true,
            selectedVideoInputId: '',
            selectedAudioInputId: '',
            availableVideoInputs: [],
            availableAudioInputs: [],
            mediaInputsLoading: false,
            switchingVideoInput: false,
            switchingAudioInput: false,
          },
        }),
      ),
    ).toBe(false)
  })

  it('supports sip trunk mode when trunk id is valid', () => {
    expect(
      canPlaceCall(
        createState({
          mode: 'siptrunk',
          trunk: {
            status: 'resolved',
            credentials: {
              trunkId: '88',
              sipDomain: 'example.com',
              sipUsername: '1001',
              sipPassword: 'secret',
              sipPort: 5060,
            },
          },
        }),
      ),
    ).toBe(true)
  })

  it('returns false in sip trunk mode when trunk is not resolved', () => {
    expect(
      canPlaceCall(
        createState({
          mode: 'siptrunk',
          trunk: {
            status: 'not-ready',
            credentials: {
              trunkId: '88',
              sipDomain: '',
              sipUsername: '',
              sipPassword: '',
              sipPort: 5060,
            },
          },
        }),
      ),
    ).toBe(false)
  })

  it('supports sip trunk mode when trunk public id is uuid', () => {
    expect(
      canPlaceCall(
        createState({
          mode: 'siptrunk',
          trunk: {
            status: 'resolved',
            credentials: {
              trunkId: '8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a',
              sipDomain: '',
              sipUsername: '',
              sipPassword: '',
              sipPort: 5060,
            },
          },
        }),
      ),
    ).toBe(true)
  })
})

describe('call state normalization', () => {
  it('normalizes reconnecting state', () => {
    expect(normalizeCallStatus('reconnecting')).toBe('reconnecting')
  })

  it('treats reconnecting as in-progress call', () => {
    expect(
      isCallInProgress(
        createState({
          call: {
            sessionId: 's1',
            state: 'reconnecting',
            elapsedSeconds: 3,
            callCount: 1,
            translatorEnabled: false,
            translatorSrcLang: '',
            translatorTgtLang: '',
          },
        }),
      ),
    ).toBe(true)
  })
})

describe('canResolveTrunk', () => {
  it('returns false when websocket is not connected', () => {
    expect(
      canResolveTrunk(
        createState({
          connection: { status: 'disconnected', wsStateText: 'Disconnected' },
          mode: 'siptrunk',
          trunk: {
            status: 'not-resolved',
            credentials: {
              trunkId: '88',
              sipDomain: '',
              sipUsername: '',
              sipPassword: '',
              sipPort: 5060,
            },
          },
        }),
      ),
    ).toBe(false)
  })

  it('returns false when no trunk identifier is selected', () => {
    expect(
      canResolveTrunk(
        createState({
          mode: 'siptrunk',
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
        }),
      ),
    ).toBe(false)
  })

  it('supports resolve when trunk id is provided', () => {
    expect(
      canResolveTrunk(
        createState({
          mode: 'siptrunk',
          trunk: {
            status: 'not-resolved',
            credentials: {
              trunkId: '88',
              sipDomain: '',
              sipUsername: '',
              sipPassword: '',
              sipPort: 5060,
            },
          },
        }),
      ),
    ).toBe(true)
  })

  it('supports resolve when trunk public id is uuid', () => {
    expect(
      canResolveTrunk(
        createState({
          mode: 'siptrunk',
          trunk: {
            status: 'not-resolved',
            credentials: {
              trunkId: '8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a',
              sipDomain: '',
              sipUsername: '',
              sipPassword: '',
              sipPort: 5060,
            },
          },
        }),
      ),
    ).toBe(true)
  })
})
