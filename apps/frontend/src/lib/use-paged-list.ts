import { useCallback, useEffect, useRef, useState } from 'react'

export interface PagedResult<T> {
  items: Array<T>
  total: number
}

/**
 * Paged list loader. `loading` is only true until the first response, so
 * refreshes and page changes keep the current rows on screen (no flashing
 * spinner); `refreshing` covers every request.
 */
export function usePagedList<T>(
  fetchPage: (page: number, pageSize: number) => Promise<PagedResult<T>>,
  deps: ReadonlyArray<unknown>,
  defaultPageSize = 50,
) {
  const [items, setItems] = useState<Array<T>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSizeState] = useState(defaultPageSize)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const fetchRef = useRef(fetchPage)
  fetchRef.current = fetchPage
  const requestRef = useRef(0)

  const depsKey = JSON.stringify(deps)

  // Reset to the first page when the query (deps) changes.
  const prevDeps = useRef(depsKey)
  useEffect(() => {
    if (prevDeps.current !== depsKey) {
      prevDeps.current = depsKey
      setPage(1)
    }
  }, [depsKey])

  const reload = useCallback(async () => {
    const requestId = ++requestRef.current
    setRefreshing(true)
    try {
      const res = await fetchRef.current(page, pageSize)
      if (requestId !== requestRef.current) return
      setItems(res.items)
      setTotal(res.total)
      setError(null)
    } catch (err) {
      if (requestId !== requestRef.current) return
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      if (requestId === requestRef.current) {
        setRefreshing(false)
        setLoading(false)
      }
    }
    // depsKey covers caller-provided query inputs.
  }, [page, pageSize, depsKey])

  useEffect(() => {
    void reload()
  }, [reload])

  const setPageSize = useCallback((next: number) => {
    setPageSizeState(next)
    setPage(1)
  }, [])

  return {
    items,
    total,
    page,
    setPage,
    pageSize,
    setPageSize,
    totalPages: Math.max(1, Math.ceil(total / pageSize)),
    loading,
    refreshing,
    error,
    reload,
  }
}
