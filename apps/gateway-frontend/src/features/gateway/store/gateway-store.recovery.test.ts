import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { gatewayActions, gatewayStore } from './gateway-store'

const TEST_TRUNK_PUBLIC_ID = '8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a'
const originalWindow = (globalThis as { window?: unknown }).window
const originalWebSocket = globalThis.WebSocket
const originalLocalStorage = (globalThis as { localStorage?: unknown })
  .localStorage

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

describe('gateway recovery signaling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []

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
})
