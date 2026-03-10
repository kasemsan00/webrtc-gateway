import {
  RiArrowDownLine,
  RiArrowUpLine,
  RiHistoryLine,
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { useNavigate } from '@tanstack/react-router'
import { debounce } from '@tanstack/pacer'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type {
  SessionHistory,
  SessionHistoryListParams,
} from '@/features/session-history/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { formatThaiDateTime } from '@/lib/date-time'
import { useTheme } from '@/lib/theme'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import { fetchSessionHistory } from '@/features/session-history/services/session-history-api'
import { subscribeSessionEvents } from '@/features/active-sessions/services/active-sessions-api'

const DEFAULT_PAGE_SIZE = 20

function formatDuration(createdAt: string, endedAt: string) {
  if (!createdAt || !endedAt) return '-'
  try {
    const start = new Date(createdAt).getTime()
    const end = new Date(endedAt).getTime()
    const diff = Math.max(0, Math.floor((end - start) / 1000))
    const m = Math.floor(diff / 60)
    const s = diff % 60
    return m > 0 ? `${m}m ${s}s` : `${s}s`
  } catch {
    return '-'
  }
}

function DirectionBadge({ direction }: { direction: string }) {
  if (direction === 'inbound') {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px]">
        <RiArrowDownLine className="size-2.5" />
        Inbound
      </Badge>
    )
  }
  return (
    <Badge variant="default" className="gap-1 text-[10px]">
      <RiArrowUpLine className="size-2.5" />
      Outbound
    </Badge>
  )
}

function StateBadge({ state }: { state: string }) {
  switch (state) {
    case 'active':
      return (
        <Badge variant="success" className="text-[10px]">
          Active
        </Badge>
      )
    case 'ended':
      return (
        <Badge variant="secondary" className="text-[10px]">
          Ended
        </Badge>
      )
    case 'connecting':
      return (
        <Badge
          variant="default"
          className="bg-amber-600/20 text-[10px] text-amber-400"
        >
          Connecting
        </Badge>
      )
    case 'reconnecting':
      return (
        <Badge
          variant="default"
          className="bg-amber-600/20 text-[10px] text-amber-400"
        >
          Reconnecting
        </Badge>
      )
    case 'incoming':
      return (
        <Badge
          variant="default"
          className="bg-blue-600/20 text-[10px] text-blue-400"
        >
          Incoming
        </Badge>
      )
    default:
      return (
        <Badge variant="outline" className="text-[10px]">
          {state || 'Unknown'}
        </Badge>
      )
  }
}

export function SessionHistoryPage() {
  const { theme, toggleTheme } = useTheme()
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<Array<SessionHistory>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [search, setSearch] = useState('')
  const [directionFilter, setDirectionFilter] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const searchRef = useRef(search)
  searchRef.current = search

  const directionRef = useRef(directionFilter)
  directionRef.current = directionFilter

  const load = useCallback(
    async (
      params?: Partial<SessionHistoryListParams>,
      options?: { silent?: boolean },
    ) => {
      if (!options?.silent) {
        setLoading(true)
      }
      setError(null)
      try {
        const res = await fetchSessionHistory({
          page: params?.page ?? page,
          pageSize: params?.pageSize ?? pageSize,
          search: (params?.search ?? searchRef.current) || undefined,
          direction: (params?.direction ?? directionRef.current) || undefined,
        })
        setSessions(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(
          err instanceof Error ? err.message : 'Failed to fetch sessions',
        )
      } finally {
        if (!options?.silent) {
          setLoading(false)
        }
      }
    },
    [page, pageSize],
  )

  useEffect(() => {
    void load()
  }, [load])

  const loadRef = useRef(load)
  loadRef.current = load

  const handleSilentReload = useCallback(() => {
    void loadRef.current(undefined, { silent: true })
  }, [])

  useVisibilityRealtimeReload({
    subscribe: (onEvent, onError) =>
      subscribeSessionEvents(() => onEvent(), onError),
    onReload: handleSilentReload,
  })

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

  const handleDirectionChange = useCallback((dir: string) => {
    setDirectionFilter(dir)
    setPage(1)
    void loadRef.current({ page: 1, direction: dir })
  }, [])

  const handleRefresh = useCallback(async () => {
    await loadRef.current()
  }, [])

  const handlePageChange = useCallback((nextPage: number) => {
    setPage(nextPage)
  }, [])

  const handlePageSizeChange = useCallback((nextPageSize: number) => {
    setPageSize(nextPageSize)
    setPage(1)
    void loadRef.current({ page: 1, pageSize: nextPageSize })
  }, [])

  const columns = useMemo<Array<ColumnDef<SessionHistory>>>(
    () => [
      {
        accessorKey: 'sessionId',
        header: 'Session ID',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-muted-foreground">
            {row.original.sessionId}
          </span>
        ),
      },
      {
        accessorKey: 'direction',
        header: 'Direction',
        cell: ({ row }) => (
          <DirectionBadge direction={row.original.direction} />
        ),
      },
      {
        accessorKey: 'fromUri',
        header: 'From',
        cell: ({ row }) => (
          <span
            className="inline-block max-w-[180px] truncate text-xs"
            title={row.original.fromUri}
          >
            {row.original.fromUri || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'toUri',
        header: 'To',
        cell: ({ row }) => (
          <span
            className="inline-block max-w-[180px] truncate text-xs"
            title={row.original.toUri}
          >
            {row.original.toUri || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'finalState',
        header: 'State',
        cell: ({ row }) => <StateBadge state={row.original.finalState} />,
      },
      {
        accessorKey: 'endReason',
        header: 'End Reason',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.endReason || '-'}
          </span>
        ),
      },
      {
        id: 'duration',
        header: 'Duration',
        cell: ({ row }) => (
          <span className="text-xs">
            {formatDuration(row.original.createdAt, row.original.endedAt)}
          </span>
        ),
      },
      {
        accessorKey: 'sipCallId',
        header: 'SIP Call-ID',
        cell: ({ row }) => (
          <span
            className="inline-block max-w-[140px] truncate font-mono text-[10px] text-muted-foreground"
            title={row.original.sipCallId}
          >
            {row.original.sipCallId || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'createdAt',
        header: 'Created',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {formatThaiDateTime(row.original.createdAt)}
          </span>
        ),
      },
      {
        accessorKey: 'endedAt',
        header: 'Ended',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {formatThaiDateTime(row.original.endedAt)}
          </span>
        ),
      },
    ],
    [],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          {/* Search */}
          <Input
            className="h-7 w-52 text-xs"
            placeholder="Search session, from, to, call-id..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              debouncedSearch(e.target.value)
            }}
          />
          <Separator orientation="vertical" className="h-4" />
          {/* Direction filter */}
          <div className="flex items-center gap-0.5">
            <Button
              size="sm"
              variant={directionFilter === '' ? 'secondary' : 'ghost'}
              className="h-7 px-2 text-xs"
              onClick={() => handleDirectionChange('')}
            >
              All
            </Button>
            <Button
              size="sm"
              variant={directionFilter === 'inbound' ? 'secondary' : 'ghost'}
              className="h-7 px-2 text-xs"
              onClick={() => handleDirectionChange('inbound')}
            >
              Inbound
            </Button>
            <Button
              size="sm"
              variant={directionFilter === 'outbound' ? 'secondary' : 'ghost'}
              className="h-7 px-2 text-xs"
              onClick={() => handleDirectionChange('outbound')}
            >
              Outbound
            </Button>
          </div>
          <Separator orientation="vertical" className="h-4" />
          {/* Refresh */}
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={handleRefresh}
            disabled={loading}
          >
            <RiRefreshLine
              className={`size-3.5 ${loading ? 'animate-spin' : ''}`}
            />
            Refresh
          </Button>
          <Separator orientation="vertical" className="h-4" />
          {/* Theme toggle */}
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            onClick={toggleTheme}
            aria-label={
              theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'
            }
          >
            {theme === 'dark' ? (
              <RiSunLine className="size-3.5" />
            ) : (
              <RiMoonLine className="size-3.5" />
            )}
          </Button>
        </div>
      </Header>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4">
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : sessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiHistoryLine className="size-10 opacity-30" />
            <p className="text-sm">No call sessions found</p>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={sessions}
            onRowClick={(sess) =>
              navigate({
                to: '/sessions/$sessionId',
                params: { sessionId: sess.sessionId },
              })
            }
            getRowClassName={() => 'hover:bg-muted/50'}
          />
        )}

        <ServerPaginationControls
          page={page}
          totalPages={totalPages}
          total={total}
          pageSize={pageSize}
          onPageChange={handlePageChange}
          onPageSizeChange={handlePageSizeChange}
          totalLabel="total"
        />
      </div>
    </div>
  )
}
