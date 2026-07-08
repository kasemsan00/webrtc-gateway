import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchLogFiles, fetchLogTail } from './gateway-logs-api'

describe('gateway-logs-api', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches log file list', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: () => ({
          items: [
            {
              name: 'k2-gateway-2026-06-26.log',
              size: 1024,
              modifiedAt: '2026-06-26T10:00:00Z',
              current: true,
            },
          ],
        }),
      }),
    )

    const result = await fetchLogFiles()
    expect(result.items).toHaveLength(1)
    expect(result.items[0]?.current).toBe(true)
  })

  it('fetches current log tail', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: () => ({
          name: 'k2-gateway-2026-06-26.log',
          current: true,
          tail: 2,
          lines: ['line-1', 'line-2'],
          truncated: false,
        }),
      }),
    )

    const result = await fetchLogTail({ tail: 2 })
    expect(result.lines).toEqual(['line-1', 'line-2'])
  })
})
