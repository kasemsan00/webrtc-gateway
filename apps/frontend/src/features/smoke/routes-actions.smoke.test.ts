import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it, vi } from 'vitest'

const fetchJsonMock = vi.fn()

vi.mock('@/lib/http-client', () => ({
  fetchJson: fetchJsonMock,
  buildAuthHeaders: vi.fn((headers?: Record<string, string>) => headers ?? {}),
  resolveGatewayApiBaseUrl: vi.fn(() => 'http://gateway.local/api'),
}))

describe('gateway smoke route and action contracts', () => {
  it('keeps required route contracts for admin pages', () => {
    const routeTreePath = resolve(process.cwd(), 'src/routeTree.gen.ts')
    const routeTreeContent = readFileSync(routeTreePath, 'utf8')

    expect(routeTreeContent.includes("'/trunks'")).toBe(true)
    expect(routeTreeContent.includes("'/sessions'")).toBe(true)
    expect(routeTreeContent.includes("'/sessions/$sessionId'")).toBe(true)
    expect(routeTreeContent.includes("'/logs'")).toBe(true)
    expect(routeTreeContent.includes("'/client-diagnostics'")).toBe(true)
    expect(routeTreeContent.includes("'/settings'")).toBe(true)
  })

  it('keeps trunks and sessions API action wiring', async () => {
    const { fetchTrunks, refreshTrunks } =
      await import('@/features/trunk/services/trunk-api')
    const { fetchSessionHistory } =
      await import('@/features/session-history/services/session-history-api')
    const { fetchLogFiles } =
      await import('@/features/gateway-logs/services/gateway-logs-api')
    const { fetchClientDiagnostics } =
      await import('@/features/client-diagnostics/services/client-diagnostics-api')
    const { fetchGatewayConfig } =
      await import('@/features/gateway-config/services/gateway-config-api')

    fetchJsonMock.mockResolvedValueOnce({
      items: [],
      total: 0,
      page: 1,
      pageSize: 20,
    })
    await fetchTrunks({ page: 2, pageSize: 20, search: 'alice' })
    expect(fetchJsonMock).toHaveBeenCalledWith(
      expect.stringContaining('/trunks?page=2&pageSize=20&search=alice'),
    )

    fetchJsonMock.mockResolvedValueOnce({ status: 'ok' })
    await refreshTrunks()
    expect(fetchJsonMock).toHaveBeenCalledWith(
      'http://gateway.local/api/trunks/refresh',
      { method: 'POST' },
    )

    fetchJsonMock.mockResolvedValueOnce({
      items: [],
      total: 0,
      page: 1,
      pageSize: 20,
    })
    await fetchSessionHistory({ page: 1, pageSize: 20, direction: 'outbound' })
    expect(fetchJsonMock).toHaveBeenCalledWith(
      expect.stringContaining(
        '/sessions/history?page=1&pageSize=20&direction=outbound',
      ),
    )

    fetchJsonMock.mockResolvedValueOnce({ items: [] })
    await fetchLogFiles()
    expect(fetchJsonMock).toHaveBeenCalledWith('http://gateway.local/api/logs')

    fetchJsonMock.mockResolvedValueOnce({
      items: [],
      total: 0,
      page: 1,
      pageSize: 20,
    })
    await fetchClientDiagnostics({ page: 1, pageSize: 20, level: 'error' })
    expect(fetchJsonMock).toHaveBeenCalledWith(
      expect.stringContaining(
        '/client-diagnostics?page=1&pageSize=20&level=error',
      ),
    )

    fetchJsonMock.mockResolvedValueOnce({
      instanceId: 'gw-test',
      source: 'env',
      sections: {},
    })
    await fetchGatewayConfig()
    expect(fetchJsonMock).toHaveBeenCalledWith('http://gateway.local/api/config')
  })
})
