import { debounce } from '@tanstack/pacer'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

export interface ServerListState<T> {
  items: Array<T>
  total: number
  page: number
  pageSize: number
  search: string
  loading: boolean
  error: string | null
  totalPages: number
}

export interface ServerListActions {
  setPage: (page: number) => void
  setPageSize: (pageSize: number) => void
  setSearch: (search: string) => void
  handlePageChange: (page: number) => void
  handlePageSizeChange: (pageSize: number) => void
  debouncedSearch: (value: string) => void
  reload: () => void
}

export interface ServerListBaseParams {
  page?: number
  pageSize?: number
  search?: string
}

type FetchFn<T, TParams extends ServerListBaseParams> = (
  params: TParams,
) => Promise<{ items: Array<T>; total: number }>

interface UseServerListControllerOptions<TParams extends ServerListBaseParams> {
  defaultPageSize?: number
  extraParams?: Omit<TParams, keyof ServerListBaseParams>
}

export function useServerListController<
  T,
  TParams extends ServerListBaseParams,
>(
  fetchFn: FetchFn<T, TParams>,
  options: UseServerListControllerOptions<TParams> = {},
): ServerListState<T> & ServerListActions {
  const { defaultPageSize = 20, extraParams } = options

  const [items, setItems] = useState<Array<T>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(defaultPageSize)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const searchRef = useRef(search)
  searchRef.current = search

  const extraParamsRef = useRef(extraParams)
  extraParamsRef.current = extraParams

  const load = useCallback(
    async (overrides?: Partial<ServerListBaseParams>) => {
      setLoading(true)
      setError(null)
      try {
        const params = {
          ...extraParamsRef.current,
          page: overrides?.page ?? page,
          pageSize: overrides?.pageSize ?? pageSize,
          search: (overrides?.search ?? searchRef.current) || undefined,
        } as TParams
        const res = await fetchFn(params)
        setItems(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch data')
      } finally {
        setLoading(false)
      }
    },
    [page, pageSize, fetchFn],
  )

  useEffect(() => {
    void load()
  }, [load])

  const loadRef = useRef(load)
  loadRef.current = load

  const debouncedSearch = useMemo(
    () =>
      debounce(
        (value: string) => {
          setPage(1)
          void loadRef.current({ page: 1, search: value })
        },
        { wait: 300 },
      ),
    [],
  )

  const handlePageChange = useCallback((nextPage: number) => {
    setPage(nextPage)
  }, [])

  const handlePageSizeChange = useCallback((nextPageSize: number) => {
    setPageSize(nextPageSize)
    setPage(1)
    void loadRef.current({ page: 1, pageSize: nextPageSize })
  }, [])

  const reload = useCallback(() => {
    void loadRef.current()
  }, [])

  return {
    items,
    total,
    page,
    pageSize,
    search,
    loading,
    error,
    totalPages,
    setPage,
    setPageSize,
    setSearch,
    handlePageChange,
    handlePageSizeChange,
    debouncedSearch,
    reload,
  }
}
