import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { GatewayConsolePage } from './gateway-console-page'
import type { GatewayState } from '../types'

const {
  fetchTrunksMock,
  useStoreMock,
  canResolveTrunkMock,
  canPlaceCallMock,
  isCallInProgressMock,
  gatewayActionsMock,
  gatewayStoreMock,
} = vi.hoisted(() => ({
  fetchTrunksMock: vi.fn(),
  useStoreMock: vi.fn(),
  canResolveTrunkMock: vi.fn(),
  canPlaceCallMock: vi.fn(),
  isCallInProgressMock: vi.fn(),
  gatewayActionsMock: {
    initialize: vi.fn(),
    cleanup: vi.fn(),
    connect: vi.fn(),
    disconnect: vi.fn(),
    toggleConnect: vi.fn(),
    startSession: vi.fn(),
    endSession: vi.fn(),
    makeCall: vi.fn(),
    hangup: vi.fn(),
    resolveTrunk: vi.fn(),
    sendPing: vi.fn(),
    sendDTMF: vi.fn(),
    sendSwitch: vi.fn(),
    acceptCall: vi.fn(),
    rejectCall: vi.fn(),
    sendSIPMessage: vi.fn(() => true),
    updateOutgoingRttDraft: vi.fn(),
    clearLogs: vi.fn(),
    clearMessages: vi.fn(),
    toggleDialpad: vi.fn(),
    refreshMediaInputDevices: vi.fn(),
    setSelectedVideoInput: vi.fn(),
    setSelectedAudioInput: vi.fn(),
    toggleMuteAudio: vi.fn(),
    toggleMuteVideo: vi.fn(),
    toggleStats: vi.fn(),
    setMode: vi.fn(),
    setDestination: vi.fn(),
    setPublicCredential: vi.fn(),
    setTrunkCredential: vi.fn(),
    setVrsConfigField: vi.fn(),
    setInputPreset: vi.fn(),
  },
  gatewayStoreMock: {
    state: {} as GatewayState,
  },
}))

vi.mock('@tanstack/react-store', () => ({
  useStore: useStoreMock,
}))

vi.mock('@/features/trunk/services/trunk-api', () => ({
  fetchTrunks: fetchTrunksMock,
}))

vi.mock('@/features/gateway/store/gateway-store', () => ({
  canPlaceCall: canPlaceCallMock,
  canResolveTrunk: canResolveTrunkMock,
  isCallInProgress: isCallInProgressMock,
  gatewayActions: gatewayActionsMock,
  gatewayStore: gatewayStoreMock,
}))

vi.mock('@/lib/theme', () => ({
  useTheme: () => ({
    theme: 'light',
    toggleTheme: vi.fn(),
  }),
}))

vi.mock('@/components/Header', () => ({
  default: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
}))

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
    ...overrides,
  }
}

describe('GatewayConsolePage trunk selection UI', () => {
  afterEach(() => {
    cleanup()
  })

  beforeEach(() => {
    vi.clearAllMocks()

    const state = createState()
    gatewayStoreMock.state = state

    useStoreMock.mockImplementation((_store, selector) => selector(state))
    canPlaceCallMock.mockReturnValue(true)
    canResolveTrunkMock.mockImplementation((s: GatewayState) =>
      Boolean(s.trunk.credentials.trunkId),
    )
    isCallInProgressMock.mockReturnValue(false)

    fetchTrunksMock.mockResolvedValue({
      items: [
        {
          id: 88,
          public_id: '8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a',
          name: 'Main Trunk',
          domain: 'sip.example.com',
          port: 5060,
          username: '1001',
          transport: 'tcp',
          enabled: true,
          isDefault: false,
          activeCallCount: 0,
          leaseOwner: '',
          leaseUntil: '',
          lastRegisteredAt: '',
          isRegistered: false,
          lastError: '',
          createdAt: '',
          updatedAt: '',
        },
      ],
      total: 1,
      page: 1,
      pageSize: 200,
    })
  })

  it('renders trunk dropdown and removes legacy trunk credential fields', async () => {
    render(<GatewayConsolePage />)

    await waitFor(() => {
      expect(fetchTrunksMock).toHaveBeenCalled()
    })

    expect(screen.getByText('Trunk ID / UUID')).toBeTruthy()
    expect(screen.queryByPlaceholderText('sip.example.com')).toBeNull()
    expect(screen.queryByPlaceholderText('1001')).toBeNull()
    expect(screen.queryByPlaceholderText('5060 (0 = SRV)')).toBeNull()
  })

  it('disables resolve when no trunk is selected and enables/calls action when selected', async () => {
    const stateWithoutTrunk = createState({
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
    })
    gatewayStoreMock.state = stateWithoutTrunk
    useStoreMock.mockImplementation((_store, selector) =>
      selector(stateWithoutTrunk),
    )
    canResolveTrunkMock.mockReturnValue(false)

    const { rerender } = render(<GatewayConsolePage />)
    const resolveButton = await screen.findByRole('button', { name: 'Resolve' })
    expect((resolveButton as HTMLButtonElement).disabled).toBe(true)

    const stateWithTrunk = createState({
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
    })
    gatewayStoreMock.state = stateWithTrunk
    useStoreMock.mockImplementation((_store, selector) =>
      selector(stateWithTrunk),
    )
    canResolveTrunkMock.mockReturnValue(true)

    rerender(<GatewayConsolePage />)

    const enabledResolveButton = await screen.findByRole('button', {
      name: 'Resolve',
    })
    await waitFor(() => {
      expect((enabledResolveButton as HTMLButtonElement).disabled).toBe(false)
    })
    fireEvent.click(enabledResolveButton)
    expect(gatewayActionsMock.resolveTrunk).toHaveBeenCalled()
  })

  it('disables call button when canPlaceCall is false', async () => {
    canPlaceCallMock.mockReturnValue(false)
    render(<GatewayConsolePage />)

    const callButton = await screen.findByRole('button', { name: /call/i })
    expect((callButton as HTMLButtonElement).disabled).toBe(true)
  })
})
