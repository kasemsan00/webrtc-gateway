import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchAllTrunks } from './trunk-api'

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchAllTrunks', () => {
  it('walks pages until every trunk is loaded', async () => {
    // Server caps pages at 2 items to exercise the loop.
    const all = Array.from({ length: 5 }, (_, index) => ({ id: index + 1 }))
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input))
      const page = Number(url.searchParams.get('page'))
      return Promise.resolve(
        jsonResponse({
          items: all.slice((page - 1) * 2, page * 2),
          total: all.length,
          page,
          pageSize: 2,
        }),
      )
    })
    vi.stubGlobal('fetch', fetchMock)

    const res = await fetchAllTrunks({ search: 'a' })

    expect(res.items.map((trunk) => trunk.id)).toEqual([1, 2, 3, 4, 5])
    expect(res.total).toBe(5)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    const firstUrl = new URL(String(fetchMock.mock.calls[0][0]))
    expect(firstUrl.searchParams.get('pageSize')).toBe('100')
    expect(firstUrl.searchParams.get('search')).toBe('a')
  })

  it('stops on an empty page', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        jsonResponse({ items: [], total: 10, page: 1, pageSize: 100 }),
      ),
    )
    vi.stubGlobal('fetch', fetchMock)

    const res = await fetchAllTrunks()

    expect(res.items).toEqual([])
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
