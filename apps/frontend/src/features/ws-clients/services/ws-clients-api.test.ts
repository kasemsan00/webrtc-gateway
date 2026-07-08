// apps/frontend/src/features/ws-clients/services/ws-clients-api.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchWSClients } from './ws-clients-api'

describe('ws-clients-api', () => {
  afterEach(() => vi.restoreAllMocks())

  it('fetches ws clients list', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => [{ clientId: 'c1', trunkResolved: true, resolvedTrunkId: 5 }],
    })
    vi.stubGlobal('fetch', fetchMock)
    const result = await fetchWSClients()
    expect(result).toHaveLength(1)
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/ws-clients')
  })
})
