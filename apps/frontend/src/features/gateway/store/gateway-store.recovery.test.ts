import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { sendSwitchRequestMock } = vi.hoisted(() => ({
  sendSwitchRequestMock: vi.fn(),
}))

vi.mock('../services/switch-api', () => ({
  sendSwitchRequest: sendSwitchRequestMock,
}))

import { gatewayActions, gatewayStore } from './gateway-store'

const TEST_TRUNK_PUBLIC_ID = '8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a'
const originalWindow = (globalThis as { window?: unknown }).window
const originalWebSocket = globalThis.WebSocket
const originalRTCPeerConnection = globalThis.RTCPeerConnection
const originalRTCRtpSender = globalThis.RTCRtpSender
const originalMediaStream = globalThis.MediaStream
const originalLocalStorage = (globalThis as { localStorage?: unknown })
  .localStorage
const originalMediaDevices = globalThis.navigator?.mediaDevices

class MockWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3
  static instances: Array<MockWebSocket> = []

  readonly url: string
  readyState = MockWebSocket.CONNECTING
  sent: Array<string> = []

  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    if (this.readyState === MockWebSocket.CLOSED) return
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  open() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }

  emitMessage(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent<string>)
  }
}

class MockMediaStreamTrack {
  readonly kind: 'audio' | 'video'
  readonly id: string
  readyState: MediaStreamTrackState = 'live'

  constructor(kind: 'audio' | 'video') {
    this.kind = kind
    this.id = `${kind}-track-1`
  }

  stop() {
    this.readyState = 'ended'
  }

  async applyConstraints() {}
}

class MockMediaStream {
  private readonly tracks: MockMediaStreamTrack[]

  constructor(tracks: MockMediaStreamTrack[] = []) {
    this.tracks = [...tracks]
  }

  getTracks() {
    return [...this.tracks]
  }

  getAudioTracks() {
    return this.tracks.filter((track) => track.kind === 'audio')
  }

  getVideoTracks() {
    return this.tracks.filter((track) => track.kind === 'video')
  }

  addTrack(track: MockMediaStreamTrack) {
    this.tracks.push(track)
  }
}

class MockRTCPeerConnection {
  localDescription: { type: 'offer'; sdp: string } | null = null
  iceGatheringState: RTCIceGatheringState = 'complete'
  iceConnectionState: RTCIceConnectionState = 'new'
  signalingState: RTCSignalingState = 'stable'
  oniceconnectionstatechange: (() => void) | null = null
  onsignalingstatechange: (() => void) | null = null
  ontrack: ((event: RTCTrackEvent) => void) | null = null
  private readonly listeners = new Map<string, Set<() => void>>()

  addTrack() {}

  getTransceivers() {
    return []
  }

  getSenders() {
    return []
  }

  async createOffer() {
    return { type: 'offer', sdp: 'v=0' } as RTCSessionDescriptionInit
  }

  async setLocalDescription(description: RTCSessionDescriptionInit) {
    this.localDescription = {
      type: 'offer',
      sdp: description.sdp ?? 'v=0',
    }
  }

  async setRemoteDescription() {}

  async getStats() {
    return new Map()
  }

  close() {}

  addEventListener(event: string, cb: () => void) {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, new Set())
    }
    this.listeners.get(event)?.add(cb)
  }

  removeEventListener(event: string, cb: () => void) {
    this.listeners.get(event)?.delete(cb)
  }
}

async function flushAsync() {
  await Promise.resolve()
  await Promise.resolve()
}

async function waitForOffer(ws: MockWebSocket) {
  for (let idx = 0; idx < 20; idx += 1) {
    if (ws.sent.some((raw) => raw.includes('"type":"offer"'))) {
      return
    }
    await flushAsync()
  }
  throw new Error('offer was not sent in time')
}

describe('gateway recovery signaling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
    sendSwitchRequestMock.mockReset()

    Object.defineProperty(globalThis, 'window', {
      value: {
        location: {
          protocol: 'http:',
          hostname: 'localhost',
        },
        localStorage: originalLocalStorage,
      },
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'WebSocket', {
      value: MockWebSocket,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'MediaStream', {
      value: MockMediaStream,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'RTCPeerConnection', {
      value: MockRTCPeerConnection,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'RTCRtpSender', {
      value: {
        getCapabilities: vi.fn(() => ({ codecs: [] })),
      },
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis.navigator, 'mediaDevices', {
      value: {
        enumerateDevices: vi.fn(async () => []),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        getUserMedia: vi.fn(async () =>
          new MockMediaStream([
            new MockMediaStreamTrack('audio'),
            new MockMediaStreamTrack('video'),
          ]),
        ),
      },
      configurable: true,
      writable: true,
    })

    gatewayActions.cleanup()
  })

  afterEach(() => {
    gatewayActions.cleanup()
    vi.useRealTimers()

    if (originalWindow === undefined) {
      Reflect.deleteProperty(globalThis, 'window')
    } else {
      Object.defineProperty(globalThis, 'window', {
        value: originalWindow,
        configurable: true,
        writable: true,
      })
    }

    Object.defineProperty(globalThis, 'WebSocket', {
      value: originalWebSocket,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'MediaStream', {
      value: originalMediaStream,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'RTCPeerConnection', {
      value: originalRTCPeerConnection,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(globalThis, 'RTCRtpSender', {
      value: originalRTCRtpSender,
      configurable: true,
      writable: true,
    })
    if (originalMediaDevices === undefined) {
      Reflect.deleteProperty(globalThis.navigator, 'mediaDevices')
    } else {
      Object.defineProperty(globalThis.navigator, 'mediaDevices', {
        value: originalMediaDevices,
        configurable: true,
        writable: true,
      })
    }

    if (originalLocalStorage === undefined) {
      Reflect.deleteProperty(globalThis, 'localStorage')
    } else {
      Object.defineProperty(globalThis, 'localStorage', {
        value: originalLocalStorage,
        configurable: true,
        writable: true,
      })
    }
  })

  it('redirects and reconnects when resume_redirect is received', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'session-1',
        state: 'active',
      },
    }))

    ws.emitMessage({
      type: 'resume_redirect',
      sessionId: 'session-1',
      redirectUrl: 'ws://node-b:8080/ws',
    })

    vi.advanceTimersByTime(250)

    expect(MockWebSocket.instances.length).toBe(2)
    expect(MockWebSocket.instances[1].url).toContain('ws://node-b:8080/ws')
    expect(gatewayStore.state.connection.status).toBe('reconnecting')
    expect(gatewayStore.state.call.state).toBe('reconnecting')
    expect(gatewayStore.state.call.sessionId).toBe('session-1')
  })

  it('marks call active when resumed arrives', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'session-2',
        state: 'reconnecting',
      },
    }))

    ws.emitMessage({
      type: 'resumed',
      sessionId: 'session-2',
      sdp: 'v=0',
      state: 'reconnecting',
    })

    expect(gatewayStore.state.call.sessionId).toBe('session-2')
    expect(gatewayStore.state.call.state).toBe('active')
  })

  it('ends and clears session when resume_failed arrives', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'session-3',
        state: 'reconnecting',
      },
    }))

    ws.emitMessage({
      type: 'resume_failed',
      reason: 'Session not found or expired',
    })

    expect(gatewayStore.state.call.state).toBe('idle')
    expect(gatewayStore.state.call.sessionId).toBeNull()
  })

  it('accepts trunk_resolved with trunkPublicId only', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    ws.emitMessage({
      type: 'trunk_resolved',
      trunkPublicId: TEST_TRUNK_PUBLIC_ID,
    })

    expect(gatewayStore.state.trunk.status).toBe('resolved')
    expect(gatewayStore.state.trunk.credentials.trunkId).toBe(
      TEST_TRUNK_PUBLIC_ID,
    )
  })

  it('prefers trunkPublicId over trunkId when both are provided', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    ws.emitMessage({
      type: 'trunk_resolved',
      trunkId: 88,
      trunkPublicId: TEST_TRUNK_PUBLIC_ID,
    })

    expect(gatewayStore.state.trunk.status).toBe('resolved')
    expect(gatewayStore.state.trunk.credentials.trunkId).toBe(
      TEST_TRUNK_PUBLIC_ID,
    )
  })

  it('sends websocket trunk_resolve when resolving by trunkId', async () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    gatewayStore.setState((state) => ({
      ...state,
      mode: 'siptrunk',
      trunk: {
        ...state.trunk,
        credentials: {
          ...state.trunk.credentials,
          trunkId: '88',
        },
      },
    }))

    await gatewayActions.resolveTrunk()

    expect(
      ws.sent.some(
        (raw) =>
          raw.includes('"type":"trunk_resolve"') &&
          raw.includes('"trunkId":88'),
      ),
    ).toBe(true)
  })

  it('re-resolves trunk on reconnect when trunk is already resolved', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws1 = MockWebSocket.instances[0]
    ws1.open()

    gatewayStore.setState((state) => ({
      ...state,
      mode: 'siptrunk',
      trunk: {
        ...state.trunk,
        status: 'resolved',
        credentials: {
          ...state.trunk.credentials,
          trunkId: '88',
        },
      },
    }))

    ws1.close()
    vi.advanceTimersByTime(20000)

    expect(MockWebSocket.instances.length).toBeGreaterThanOrEqual(2)
    const ws2 = MockWebSocket.instances[1]
    ws2.open()

    expect(
      ws2.sent.some(
        (raw) =>
          raw.includes('"type":"trunk_resolve"') &&
          raw.includes('"trunkId":88'),
      ),
    ).toBe(true)
  })

  it('guards duplicate accept clicks while preparing incoming media session', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    gatewayStore.setState((state) => ({
      ...state,
      incomingCall: {
        from: 'sip:1100200363490@203.150.245.42',
        to: 'sip:00025@203.151.21.121:5090',
        mode: 'siptrunk',
        sessionId: 'incoming-1',
      },
    }))

    gatewayActions.acceptCall()
    gatewayActions.acceptCall()

    expect(gatewayStore.state.incomingAction).toBe('preparing_accept')
    expect(
      gatewayStore.state.logs.filter((entry) =>
        entry.message.includes(
          'Incoming call requires local media session. Preparing automatically before accept...',
        ),
      ).length,
    ).toBe(1)
    expect(
      gatewayStore.state.logs.some((entry) =>
        entry.message.includes('Incoming call action already in progress'),
      ),
    ).toBe(true)
    expect(
      ws.sent.filter(
        (raw) =>
          raw.includes('"type":"accept"') && raw.includes('"sessionId":"incoming-1"'),
      ).length,
    ).toBe(0)
  })

  it('auto-sends switch once after incoming accept reaches active state', async () => {
    sendSwitchRequestMock.mockResolvedValueOnce({
      status: 'accepted',
      sessionId: 'incoming-auto-1',
      autoMode: true,
    })

    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()
    await gatewayActions.startSession()
    await waitForOffer(ws)

    ws.emitMessage({
      type: 'incoming',
      from: 'sip:caller@test',
      to: 'sip:receiver@test',
      sessionId: 'incoming-auto-1',
    })
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'local-session-1',
      },
    }))

    gatewayActions.acceptCall()
    await flushAsync()

    ws.emitMessage({
      type: 'state',
      state: 'active',
      sessionId: 'incoming-auto-1',
    })
    ws.emitMessage({
      type: 'state',
      state: 'active',
      sessionId: 'incoming-auto-1',
    })
    await flushAsync()

    expect(sendSwitchRequestMock).toHaveBeenCalledTimes(1)
    expect(sendSwitchRequestMock).toHaveBeenCalledWith({
      sessionId: 'incoming-auto-1',
    })
  })

  it('does not auto-send switch if call ends before first active state', async () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()
    await gatewayActions.startSession()
    await waitForOffer(ws)

    ws.emitMessage({
      type: 'incoming',
      from: 'sip:caller@test',
      to: 'sip:receiver@test',
      sessionId: 'incoming-auto-2',
    })
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'local-session-2',
      },
    }))

    gatewayActions.acceptCall()
    await flushAsync()

    ws.emitMessage({
      type: 'state',
      state: 'ended',
      sessionId: 'incoming-auto-2',
    })
    ws.emitMessage({
      type: 'state',
      state: 'active',
      sessionId: 'incoming-auto-2',
    })
    await flushAsync()

    expect(sendSwitchRequestMock).not.toHaveBeenCalled()
  })

  it('logs auto-switch error once when background send fails', async () => {
    sendSwitchRequestMock.mockRejectedValueOnce(new Error('switch failed'))

    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()
    await gatewayActions.startSession()
    await waitForOffer(ws)

    ws.emitMessage({
      type: 'incoming',
      from: 'sip:caller@test',
      to: 'sip:receiver@test',
      sessionId: 'incoming-auto-3',
    })
    gatewayStore.setState((state) => ({
      ...state,
      call: {
        ...state.call,
        sessionId: 'local-session-3',
      },
    }))

    gatewayActions.acceptCall()
    await flushAsync()

    ws.emitMessage({
      type: 'state',
      state: 'active',
      sessionId: 'incoming-auto-3',
    })
    ws.emitMessage({
      type: 'state',
      state: 'active',
      sessionId: 'incoming-auto-3',
    })
    await flushAsync()

    expect(sendSwitchRequestMock).toHaveBeenCalledTimes(1)
    expect(
      gatewayStore.state.logs.some((entry) =>
        entry.message.includes('Switch request failed: switch failed'),
      ),
    ).toBe(true)
  })

  it('blocks reject while incoming action is in progress', () => {
    gatewayActions.connect('ws://node-a:8080/ws')
    const ws = MockWebSocket.instances[0]
    ws.open()

    gatewayStore.setState((state) => ({
      ...state,
      incomingCall: {
        from: 'sip:1100200363490@203.150.245.42',
        to: 'sip:00025@203.151.21.121:5090',
        mode: 'siptrunk',
        sessionId: 'incoming-2',
      },
      incomingAction: 'preparing_accept',
    }))

    gatewayActions.rejectCall()

    expect(
      ws.sent.some(
        (raw) =>
          raw.includes('"type":"reject"') && raw.includes('"sessionId":"incoming-2"'),
      ),
    ).toBe(false)
    expect(gatewayStore.state.incomingCall?.sessionId).toBe('incoming-2')
    expect(
      gatewayStore.state.logs.some((entry) =>
        entry.message.includes(
          'Incoming call is being processed. Reject is temporarily disabled.',
        ),
      ),
    ).toBe(true)
  })
})
