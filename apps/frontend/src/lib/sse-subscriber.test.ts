import { afterEach, describe, expect, it, vi } from 'vitest'

import { subscribeAuthenticatedSse } from './sse-subscriber'
import { clearAccessToken, setAccessToken } from '@/features/auth/token-store'

function createSseResponse(chunks: Array<string>) {
  let index = 0
  const encoder = new TextEncoder()

  return {
    ok: true,
    status: 200,
    body: {
      getReader: () => ({
        read: () => {
          if (index >= chunks.length) {
            return Promise.resolve({ done: true, value: undefined })
          }

          const value = encoder.encode(chunks[index])
          index += 1
          return Promise.resolve({ done: false, value })
        },
      }),
    },
  }
}

describe('subscribeAuthenticatedSse', () => {
  afterEach(() => {
    clearAccessToken()
    vi.restoreAllMocks()
  })

  it('parses named SSE events and sends bearer auth', async () => {
    setAccessToken('sse-token')
    const onEvent = vi.fn()
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        createSseResponse([
          'event: session\ndata: {"type":"session_created","sessionId":"s-1","at":"now"}\n\n',
        ]),
      )
    vi.stubGlobal('fetch', fetchMock)

    const unsubscribe = subscribeAuthenticatedSse({
      url: 'http://localhost:8000/api/sessions/stream',
      eventName: 'session',
      onEvent,
    })

    await vi.waitFor(() => {
      expect(onEvent).toHaveBeenCalledWith({
        type: 'session_created',
        sessionId: 's-1',
        at: 'now',
      })
    })

    expect(fetchMock).toHaveBeenCalledWith(
      'http://localhost:8000/api/sessions/stream',
      expect.objectContaining({
        method: 'GET',
        headers: expect.any(Headers),
      }),
    )

    const headers = fetchMock.mock.calls[0]?.[1]?.headers as Headers
    expect(headers.get('Authorization')).toBe('Bearer sse-token')
    expect(headers.get('Accept')).toBe('text/event-stream')

    unsubscribe()
  })

  it('ignores events with a different event name', async () => {
    const onEvent = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          createSseResponse([
            'event: heartbeat\ndata: {"type":"heartbeat"}\n\n',
          ]),
        ),
    )

    subscribeAuthenticatedSse({
      url: 'http://localhost:8000/api/sessions/stream',
      eventName: 'session',
      onEvent,
    })

    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(onEvent).not.toHaveBeenCalled()
  })
})
