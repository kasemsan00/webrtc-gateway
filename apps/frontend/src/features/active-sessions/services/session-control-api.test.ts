import { afterEach, describe, expect, it, vi } from 'vitest'

import { hangupSession, sendSessionDtmf } from './session-control-api'

describe('session-control-api', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('posts hangup for session id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({
        sessionId: 'sess-1',
        state: 'ended',
        message: 'Call ended',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await hangupSession('sess-1')

    expect(result.state).toBe('ended')
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/hangup/sess-1'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('posts dtmf digits', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({
        sessionId: 'sess-2',
        message: 'DTMF sent',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await sendSessionDtmf('sess-2', '123#')

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ digits: '123#' }))
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/dtmf/sess-2')
  })
})