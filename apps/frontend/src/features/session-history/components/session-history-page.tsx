import {
  RiArrowDownLine,
  RiArrowUpLine,
  RiCloseLine,
  RiHistoryLine,
  RiLoader4Line,
  RiRefreshLine,
} from '@remixicon/react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { debounce } from '@tanstack/pacer'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { SessionHistory } from '@/features/session-history/types'
import type { SessionsSearch } from '@/lib/search-params'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import {
  IdCell,
  SessionIdLink,
  TimestampCell,
} from '@/components/ui/table-cells'
import { subscribeSessionEvents } from '@/features/active-sessions/services/active-sessions-api'
import { fetchSessionHistory } from '@/features/session-history/services/session-history-api'
import {
  bangkokDayBoundsIso,
  formatDurationSec,
  formatThaiDateTime,
  secondsBetween,
} from '@/lib/date-time'
import { trunkSearchFrom } from '@/lib/operations-navigation'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import { cn } from '@/lib/utils'

const DEFAULT_PAGE_SIZE = 20

/** Final states recorded for a call, with the meaning shown to operators. */
export const SESSION_STATE_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'ended', label: 'ended: finished normally' },
  { value: 'failed', label: 'failed: setup or media failed' },
  { value: 'unknown', label: 'unknown: gateway lost track' },
  { value: 'active', label: 'active: still in progress' },
  { value: 'ringing', label: 'ringing' },
  { value: 'connecting', label: 'connecting' },
  { value: 'incoming', label: 'incoming' },
  { value: 'reconnecting', label: 'reconnecting' },
]

export const END_REASON_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'hangup', label: 'BYE / hangup (normal end)' },
  { value: 'busy', label: '486 busy' },
  { value: 'decline', label: '486 declined by user' },
  { value: 'no_answer', label: '480 no answer / offline' },
  { value: 'caller_cancelled', label: 'caller cancelled (CANCEL)' },
  { value: 'outgoing_cancelled', label: 'outgoing cancelled' },
]

function DirectionBadge({ direction }: { direction: string }) {
  if (direction === 'inbound') {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px]">
        <RiArrowDownLine className="size-2.5" />
        In
      </Badge>
    )
  }
  if (direction === 'outbound') {
    return (
      <Badge variant="outline" className="gap-1 text-[10px]">
        <RiArrowUpLine className="size-2.5" />
        Out
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="text-[10px]">
      {direction || '-'}
    </Badge>
  )
}

export function StateBadge({ state }: { state: string }) {
  const variant =
    state === 'ended'
      ? 'secondary'
      : state === 'active'
        ? 'success'
        : state === 'failed' || state === 'unknown'
          ? 'destructive'
          : 'warning'
  return (
    <Badge variant={variant} className="text-[10px]">
      {state || 'unknown'}
    </Badge>
  )
}

export function endReasonView(reason: string): {
  label: string
  variant: 'success' | 'warning' | 'destructive' | 'secondary' | 'outline'
} {
  switch (reason) {
    case 'busy':
      return { label: '486 Busy', variant: 'warning' }
    case 'decline':
      return { label: '486 Declined', variant: 'warning' }
    case 'no_answer':
    case 'offline':
      return { label: '480 No answer', variant: 'destructive' }
    case 'caller_cancelled':
      return { label: 'Caller cancelled', variant: 'secondary' }
    case 'outgoing_cancelled':
      return { label: 'Outgoing cancelled', variant: 'secondary' }
    case 'hangup':
    case 'bye':
      return { label: 'BYE / hangup', variant: 'success' }
    default:
      return { label: reason || '-', variant: 'outline' }
  }
}

export function EndReasonBadge({ reason }: { reason: string }) {
  const view = endReasonView(reason)
  return (
    <Badge
      variant={view.variant}
      className="text-[10px]"
      title={reason || undefined}
    >
      {view.label}
    </Badge>
  )
}

const SELECT_CLASS =
  'h-7 rounded-md border border-border bg-background px-2 text-xs text-foreground'

export function SessionHistoryPage() {
  const search = useSearch({ from: '/sessions/' })
  const navigate = useNavigate({ from: '/sessions/' })
  const [sessions, setSessions] = useState<Array<SessionHistory>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [queryInput, setQueryInput] = useState(search.q ?? '')
  const [initialLoading, setInitialLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const filterKey = JSON.stringify(search)

  // Keep the input in sync when the URL changes (back/forward, drill-down links).
  useEffect(() => {
    setQueryInput(search.q ?? '')
  }, [search.q])

  // Any filter change starts from page 1.
  const prevFilterKey = useRef(filterKey)
  useEffect(() => {
    if (prevFilterKey.current !== filterKey) {
      prevFilterKey.current = filterKey
      setPage(1)
    }
  }, [filterKey])

  const updateSearch = useCallback(
    (patch: Partial<SessionsSearch>) => {
      void navigate({
        search: (prev: SessionsSearch) => {
          const next: SessionsSearch = { ...prev, ...patch }
          for (const key of Object.keys(next) as Array<keyof SessionsSearch>) {
            if (!next[key]) delete next[key]
          }
          return next
        },
        replace: true,
      })
    },
    [navigate],
  )

  const load = useCallback(async () => {
    setRefreshing(true)
    try {
      const res = await fetchSessionHistory({
        page,
        pageSize,
        search: search.q,
        direction: search.direction,
        state: search.state,
        endReason: search.endReason,
        createdAfter: search.from
          ? bangkokDayBoundsIso(search.from, 'start')
          : undefined,
        createdBefore: search.to
          ? bangkokDayBoundsIso(search.to, 'end')
          : undefined,
      })
      setSessions(res.items)
      setTotal(res.total)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch sessions')
    } finally {
      setRefreshing(false)
      setInitialLoading(false)
    }
    // filterKey captures every field of `search`.
  }, [page, pageSize, filterKey])

  useEffect(() => {
    void load()
  }, [load])

  const loadRef = useRef(load)
  loadRef.current = load

  const handleSilentReload = useCallback(() => {
    void loadRef.current()
  }, [])

  useVisibilityRealtimeReload({
    name: 'sessions',
    subscribe: subscribeSessionEvents,
    onReload: handleSilentReload,
  })

  const debouncedQuery = useMemo(
    () =>
      debounce(
        (value: string) => updateSearch({ q: value.trim() || undefined }),
        {
          wait: 350,
        },
      ),
    [updateSearch],
  )

  const activeFilterCount = Object.values(search).filter(Boolean).length

  const columns = useMemo<Array<ColumnDef<SessionHistory>>>(
    () => [
      {
        accessorKey: 'sessionId',
        header: 'Session',
        cell: ({ row }) => (
          <SessionIdLink
            sessionId={row.original.sessionId}
            maxWidth="max-w-[130px]"
          />
        ),
      },
      {
        accessorKey: 'finalState',
        header: 'State',
        cell: ({ row }) => <StateBadge state={row.original.finalState} />,
      },
      {
        accessorKey: 'endReason',
        header: 'Outcome',
        cell: ({ row }) => <EndReasonBadge reason={row.original.endReason} />,
      },
      {
        id: 'call',
        header: 'Call',
        cell: ({ row }) => (
          <div className="flex min-w-0 max-w-[280px] items-center gap-1.5 text-xs">
            <DirectionBadge direction={row.original.direction} />
            <span
              className="truncate"
              title={`${row.original.fromUri} → ${row.original.toUri}`}
            >
              {row.original.fromUri || '-'}{' '}
              <span className="text-muted-foreground">→</span>{' '}
              {row.original.toUri || '-'}
            </span>
          </div>
        ),
      },
      {
        id: 'trunk',
        header: 'Trunk / Auth',
        cell: ({ row }) => {
          const s = row.original
          return (
            <div className="text-xs">
              {s.trunkId && s.trunkId > 0 ? (
                <Link
                  to="/trunks"
                  search={trunkSearchFrom({ trunkId: s.trunkId })}
                  onClick={(e) => e.stopPropagation()}
                  className="text-cyan-700 hover:underline dark:text-cyan-400"
                >
                  {s.trunkName || `#${s.trunkId}`}
                </Link>
              ) : (
                <span className="text-muted-foreground">
                  {s.trunkName || '-'}
                </span>
              )}
              <p className="text-[10px] text-muted-foreground">
                {[s.authMode, s.sipUsername].filter(Boolean).join(' · ') || ''}
              </p>
            </div>
          )
        },
      },
      {
        id: 'duration',
        header: 'Duration',
        cell: ({ row }) => {
          const seconds = row.original.endedAt
            ? secondsBetween(row.original.createdAt, row.original.endedAt)
            : null
          return (
            <span
              className="font-mono text-xs tabular-nums"
              title="From call creation to end (includes ringing)"
            >
              {seconds === null ? '-' : formatDurationSec(seconds)}
            </span>
          )
        },
      },
      {
        accessorKey: 'sipCallId',
        header: 'SIP Call-ID',
        cell: ({ row }) => (
          <IdCell
            value={row.original.sipCallId}
            label="SIP Call-ID"
            maxWidth="max-w-[120px]"
          />
        ),
      },
      {
        accessorKey: 'createdAt',
        header: 'Started',
        cell: ({ row }) => (
          <span
            title={
              row.original.endedAt
                ? `Ended ${formatThaiDateTime(row.original.endedAt)}`
                : 'Not ended'
            }
          >
            <TimestampCell value={row.original.createdAt} />
          </span>
        ),
      },
    ],
    [],
  )

  const filters = (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <Input
        className="h-7 w-full text-xs sm:w-60"
        placeholder="Search session ID, from, to, Call-ID…"
        aria-label="Search call sessions"
        value={queryInput}
        onChange={(e) => {
          setQueryInput(e.target.value)
          debouncedQuery(e.target.value)
        }}
      />
      <div
        className="flex items-center gap-0.5"
        role="group"
        aria-label="Direction"
      >
        {(
          [
            [undefined, 'All'],
            ['inbound', 'Inbound'],
            ['outbound', 'Outbound'],
          ] as const
        ).map(([value, label]) => (
          <Button
            key={label}
            size="sm"
            variant={search.direction === value ? 'secondary' : 'ghost'}
            className="h-7 px-2 text-xs"
            aria-pressed={search.direction === value}
            onClick={() => updateSearch({ direction: value })}
          >
            {label}
          </Button>
        ))}
      </div>
      <select
        className={SELECT_CLASS}
        value={search.state ?? ''}
        onChange={(e) => updateSearch({ state: e.target.value || undefined })}
        aria-label="Final state filter"
      >
        <option value="">Any state</option>
        {SESSION_STATE_OPTIONS.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      <select
        className={SELECT_CLASS}
        value={search.endReason ?? ''}
        onChange={(e) =>
          updateSearch({ endReason: e.target.value || undefined })
        }
        aria-label="Outcome filter"
      >
        <option value="">Any outcome</option>
        {END_REASON_OPTIONS.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      <label className="flex items-center gap-1 text-muted-foreground">
        From
        <Input
          type="date"
          className="h-7 w-36 text-xs"
          value={search.from ?? ''}
          onChange={(e) => updateSearch({ from: e.target.value || undefined })}
          aria-label="Started on or after (Bangkok date)"
        />
      </label>
      <label className="flex items-center gap-1 text-muted-foreground">
        To
        <Input
          type="date"
          className="h-7 w-36 text-xs"
          value={search.to ?? ''}
          onChange={(e) => updateSearch({ to: e.target.value || undefined })}
          aria-label="Started on or before (Bangkok date)"
        />
      </label>
      {activeFilterCount > 0 ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => {
            setQueryInput('')
            void navigate({ search: {}, replace: true })
          }}
        >
          <RiCloseLine className="size-3.5" />
          Clear {activeFilterCount} filter{activeFilterCount === 1 ? '' : 's'}
        </Button>
      ) : null}
    </div>
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header subBar={filters} subBarAlways>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => void load()}
          disabled={refreshing}
        >
          <RiRefreshLine
            className={`size-3.5 ${refreshing ? 'animate-spin' : ''}`}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        <p className="mb-3 text-xs text-muted-foreground">
          <strong className="text-foreground tabular-nums">{total}</strong> call
          {total === 1 ? '' : 's'}
          {activeFilterCount ? ' match the filters' : ''} · newest first · times
          in Asia/Bangkok · click a row to open the SIP timeline. The filters
          are in the URL, so you can share this view.
        </p>
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
            {error}
          </div>
        ) : null}

        {initialLoading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : sessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiHistoryLine className="size-10 opacity-30" />
            <p className="text-sm">No call sessions found</p>
            {activeFilterCount ? (
              <p className="text-xs">Try clearing some filters.</p>
            ) : (
              <p className="text-xs">
                Calls are recorded when database logging is enabled.
              </p>
            )}
          </div>
        ) : (
          <div className={cn(refreshing && 'opacity-80 transition-opacity')}>
            <DataTable
              columns={columns}
              data={sessions}
              onRowClick={(sess) =>
                void navigate({
                  to: '/sessions/$sessionId',
                  params: { sessionId: sess.sessionId },
                  search: {},
                })
              }
              getRowClassName={(sess) =>
                cn(
                  'hover:bg-muted/50',
                  (sess.finalState === 'failed' ||
                    sess.finalState === 'unknown') &&
                    'bg-rose-500/5',
                )
              }
            />
          </div>
        )}

        <ServerPaginationControls
          page={page}
          totalPages={totalPages}
          total={total}
          pageSize={pageSize}
          onPageChange={setPage}
          onPageSizeChange={(next) => {
            setPageSize(next)
            setPage(1)
          }}
          totalLabel="total"
        />
      </div>
    </div>
  )
}
