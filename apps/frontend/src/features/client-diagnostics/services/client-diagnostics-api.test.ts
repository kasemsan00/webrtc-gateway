import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  fetchClientDiagnosticPayload,
  fetchClientDiagnosticSessionEvents,
  fetchClientDiagnostics,
} from './client-diagnostics-api'

describe('client-diagnostics-api', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches diagnostics with filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({
        items: [
          {
            id: 1,
            timestamp: '2026-06-26T10:00:00.000Z',
            level: 'error',
            name: 'app.boot',
            source: 'app',
          },
        ],
        total: 1,
        page: 1,
        pageSize: 20,
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchClientDiagnostics({
      page: 1,
      pageSize: 20,
      level: 'error',
    })

    expect(result.items).toHaveLength(1)
    expect(fetchMock).toHaveBeenCalled()
    const calledUrl = String(fetchMock.mock.calls[0]?.[0])
    expect(calledUrl).toContain('level=error')
  })

  it('fetches diagnostic payload by id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({
        payloadId: 42,
        timestamp: '2026-06-26T10:00:00.000Z',
        sessionId: 'sess-1',
        kind: 'client_diagnostics',
        contentType: 'application/json',
        bodyText: '{"events":[]}',
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchClientDiagnosticPayload(42)

    expect(result.payloadId).toBe(42)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/client-diagnostics/payloads/42'),
      expect.any(Object),
    )
  })

  it('fetches session-scoped client diagnostic events', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({ items: [], total: 0, page: 1, pageSize: 50 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await fetchClientDiagnosticSessionEvents('sess-abc', {
      page: 1,
      pageSize: 50,
    })

    expect(String(fetchMock.mock.calls[0]?.[0])).toContain(
      '/client-diagnostics/sessions/sess-abc/events',
    )
  })
})
