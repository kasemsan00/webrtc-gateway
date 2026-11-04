import {
  RiFileTextLine,
  RiLoader4Line,
  RiMore2Fill,
  RiPhoneLine,
  RiRefreshLine,
  RiTranslate2,
} from '@remixicon/react'
import { Link } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { ActiveSession } from '@/features/active-sessions/types'
import type { CallTarget } from '@/features/active-sessions/components/call-control-dialogs'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import {
  IdCell,
  SessionIdLink,
  TimestampCell,
} from '@/components/ui/table-cells'
import {
  DtmfDialog,
  HangupDialog,
} from '@/features/active-sessions/components/call-control-dialogs'
import {
  fetchActiveSessions,
  subscribeSessionEvents,
} from '@/features/active-sessions/services/active-sessions-api'
import { formatDurationSec, formatRelativeTime } from '@/lib/date-time'
import { trunkSearchFrom } from '@/lib/operations-navigation'
import { useNow } from '@/lib/use-now'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import { cn } from '@/lib/utils'

const RINGING_STATES = new Set(['connecting', 'ringing', 'incoming', 'new'])

type StateFilter = 'all' | 'ringing' | 'talking'

/**
 * Live call timer. Talk time once answered, ring time before that. The server
 * `durationSec` is only used when timestamps are missing.
 */
export function liveCallTimer(
  session: Pick<
    ActiveSession,
    'answeredAt' | 'createdAt' | 'durationSec' | 'state'
  >,
  now: number,
  fetchedAt: number,
): { phase: 'talk' | 'ring'; seconds: number } {
  const answeredMs = session.answeredAt ? Date.parse(session.answeredAt) : NaN
  if (!Number.isNaN(answeredMs)) {
    return { phase: 'talk', seconds: Math.max(0, (now - answeredMs) / 1000) }
  }
  const createdMs = session.createdAt ? Date.parse(session.createdAt) : NaN
  if (RINGING_STATES.has(session.state) && !Number.isNaN(createdMs)) {
    return { phase: 'ring', seconds: Math.max(0, (now - createdMs) / 1000) }
  }
  return {
    phase: session.state === 'active' ? 'talk' : 'ring',
    seconds: session.durationSec + Math.max(0, (now - fetchedAt) / 1000),
  }
}

function stateVariant(
  state: string,
): 'default' | 'success' | 'warning' | 'destructive' | 'secondary' {
  if (state === 'active') return 'success'
  if (RINGING_STATES.has(state)) return 'warning'
  if (state === 'reconnecting') return 'destructive'
  return 'secondary'
}

function matchesSearch(session: ActiveSession, query: string): boolean {
  if (!query) return true
  const q = query.toLowerCase()
  return [
    session.id,
    session.from,
    session.to,
    session.sipCallId,
    session.trunkName,
    session.sipUsername,
    session.state,
  ].some((value) => value && value.toLowerCase().includes(q))
}

function callLabel(session: ActiveSession) {
  return `${session.from || '?'} → ${session.to || '?'}`
}

export function ActiveSessionsPage() {
  const [sessions, setSessions] = useState<Array<ActiveSession>>([])
  const [initialLoading, setInitialLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fetchedAt, setFetchedAt] = useState(() => Date.now())
  const [query, setQuery] = useState('')
  const [stateFilter, setStateFilter] = useState<StateFilter>('all')
  const [hangupTarget, setHangupTarget] = useState<CallTarget | null>(null)
  const [dtmfTarget, setDtmfTarget] = useState<CallTarget | null>(null)
  const now = useNow(1000)

  // Realtime reloads are silent: the table stays on screen while refreshing.
  const load = useCallback(async () => {
    setRefreshing(true)
    try {
      const data = await fetchActiveSessions()
      setSessions(data)
      setFetchedAt(Date.now())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch sessions')
    } finally {
      setRefreshing(false)
      setInitialLoading(false)
    }
  }, [])

  const loadRef = useRef(load)
  loadRef.current = load

  useEffect(() => {
    void load()
  }, [load])

  const handleReload = useCallback(() => {
    void loadRef.current()
  }, [])

  useVisibilityRealtimeReload({
    name: 'sessions',
    subscribe: subscribeSessionEvents,
    onReload: handleReload,
    reloadDebounceMs: 500,
    pollIntervalMs: 15_000,
  })

  const counts = useMemo(() => {
    let ringing = 0
    let talking = 0
    let inbound = 0
    let outbound = 0
    let translated = 0
    for (const s of sessions) {
      if (RINGING_STATES.has(s.state)) ringing += 1
      else if (s.state === 'active') talking += 1
      if (s.direction === 'inbound') inbound += 1
      else if (s.direction === 'outbound') outbound += 1
      if (s.translatorEnabled) translated += 1
    }
    return { ringing, talking, inbound, outbound, translated }
  }, [sessions])

  const visible = useMemo(
    () =>
      sessions.filter((s) => {
        if (stateFilter === 'ringing' && !RINGING_STATES.has(s.state)) {
          return false
        }
        if (stateFilter === 'talking' && s.state !== 'active') return false
        return matchesSearch(s, query.trim())
      }),
    [query, sessions, stateFilter],
  )

  const columns = useMemo<Array<ColumnDef<ActiveSession>>>(
    () => [
      {
        accessorKey: 'id',
        header: 'Session',
        cell: ({ row }) => <SessionIdLink sessionId={row.original.id} />,
      },
      {
        accessorKey: 'state',
        header: 'State',
        cell: ({ row }) => (
          <Badge
            variant={stateVariant(row.original.state)}
            className="text-[10px]"
          >
            {row.original.state}
          </Badge>
        ),
      },
      {
        id: 'call',
        header: 'Call',
        cell: ({ row }) => (
          <div className="min-w-0 max-w-[260px] text-xs">
            <p className="truncate" title={callLabel(row.original)}>
              <span className="mr-1 text-[10px] uppercase text-muted-foreground">
                {row.original.direction === 'inbound'
                  ? 'in'
                  : row.original.direction === 'outbound'
                    ? 'out'
                    : row.original.direction}
              </span>
              {row.original.from || '-'}{' '}
              <span className="text-muted-foreground">→</span>{' '}
              {row.original.to || '-'}
            </p>
          </div>
        ),
      },
      {
        id: 'timer',
        header: 'Time',
        cell: ({ row }) => {
          const timer = liveCallTimer(row.original, now, fetchedAt)
          return (
            <span
              className={cn(
                'whitespace-nowrap font-mono text-xs tabular-nums',
                timer.phase === 'ring' && 'text-amber-600 dark:text-amber-400',
              )}
              title={
                timer.phase === 'talk'
                  ? 'Talk time since answer'
                  : 'Ringing / setup time since the call was created'
              }
            >
              {timer.phase === 'ring' ? 'ring ' : ''}
              {formatDurationSec(timer.seconds)}
            </span>
          )
        },
      },
      {
        accessorKey: 'trunkName',
        header: 'Trunk / Auth',
        cell: ({ row }) => (
          <div className="text-xs">
            {row.original.trunkId > 0 ? (
              <Link
                to="/trunks"
                search={trunkSearchFrom({ trunkId: row.original.trunkId })}
                className="text-cyan-700 hover:underline dark:text-cyan-400"
              >
                {row.original.trunkName || `#${row.original.trunkId}`}
              </Link>
            ) : (
              <span className="text-muted-foreground">
                {row.original.trunkName || '-'}
              </span>
            )}
            <p className="text-[10px] text-muted-foreground">
              {[row.original.authMode, row.original.sipUsername]
                .filter(Boolean)
                .join(' · ') || '-'}
            </p>
          </div>
        ),
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
        accessorKey: 'translatorEnabled',
        header: 'Translator',
        cell: ({ row }) => {
          if (!row.original.translatorEnabled) {
            return <span className="text-xs text-muted-foreground">-</span>
          }
          return (
            <Badge
              variant="success"
              className="flex w-fit items-center gap-1 text-[10px]"
              title={`${row.original.translatorSrcLang ?? '?'} → ${row.original.translatorTgtLang ?? '?'}${row.original.translatorTtsVoice ? ` · voice ${row.original.translatorTtsVoice}` : ''}`}
            >
              <RiTranslate2 className="size-3" />
              {row.original.translatorSrcLang ?? '?'}→
              {row.original.translatorTgtLang ?? '?'}
            </Badge>
          )
        },
      },
      {
        accessorKey: 'createdAt',
        header: 'Started',
        cell: ({ row }) => (
          <TimestampCell value={row.original.createdAt} relative now={now} />
        ),
      },
      {
        accessorKey: 'updatedAt',
        header: 'Last update',
        cell: ({ row }) => (
          <TimestampCell value={row.original.updatedAt} relative now={now} />
        ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        cell: ({ row }) => {
          const session = row.original
          const target = { sessionId: session.id, label: callLabel(session) }
          return (
            <div className="flex justify-end gap-1">
              <Button
                size="sm"
                variant="destructive"
                className="h-6 px-2 text-[10px]"
                onClick={() => setHangupTarget(target)}
              >
                Hangup
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-6"
                    aria-label={`More actions for ${session.id}`}
                  >
                    <RiMore2Fill className="size-3.5" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-48">
                  <DropdownMenuItem asChild>
                    <Link
                      to="/sessions/$sessionId"
                      params={{ sessionId: session.id }}
                      search={{}}
                    >
                      Open session detail
                    </Link>
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => setDtmfTarget(target)}>
                    Send DTMF…
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem asChild>
                    <Link to="/logs" search={{ q: session.id }}>
                      <RiFileTextLine className="size-3.5" />
                      Logs for this session
                    </Link>
                  </DropdownMenuItem>
                  {session.sipCallId ? (
                    <DropdownMenuItem asChild>
                      <Link to="/logs" search={{ q: session.sipCallId }}>
                        <RiFileTextLine className="size-3.5" />
                        Logs for this Call-ID
                      </Link>
                    </DropdownMenuItem>
                  ) : null}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )
        },
      },
    ],
    [fetchedAt, now],
  )

  const filterButton = (value: StateFilter, label: string, count: number) => (
    <Button
      key={value}
      size="sm"
      variant={stateFilter === value ? 'secondary' : 'ghost'}
      className="h-7 gap-1 px-2 text-xs"
      onClick={() => setStateFilter(value)}
      aria-pressed={stateFilter === value}
    >
      {label}
      <span className="tabular-nums text-muted-foreground">{count}</span>
    </Button>
  )

  const filters = (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        className="h-7 w-full text-xs sm:w-56"
        placeholder="Search ID, number, Call-ID, trunk…"
        aria-label="Search active sessions"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div
        className="flex items-center gap-0.5"
        role="group"
        aria-label="State"
      >
        {filterButton('all', 'All', sessions.length)}
        {filterButton('ringing', 'Ringing', counts.ringing)}
        {filterButton('talking', 'In call', counts.talking)}
      </div>
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
        <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            <strong className="text-foreground tabular-nums">
              {sessions.length}
            </strong>{' '}
            live
          </span>
          <span>
            <strong className="text-amber-600 tabular-nums dark:text-amber-400">
              {counts.ringing}
            </strong>{' '}
            ringing / connecting
          </span>
          <span>
            <strong className="text-emerald-600 tabular-nums dark:text-emerald-400">
              {counts.talking}
            </strong>{' '}
            in call
          </span>
          <span>
            {counts.inbound} inbound · {counts.outbound} outbound
          </span>
          {counts.translated ? (
            <span>{counts.translated} translated</span>
          ) : null}
          <span className="ml-auto">
            fetched {formatRelativeTime(fetchedAt, now)}
          </span>
        </div>

        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
            {error}
            {sessions.length ? ' · showing the last loaded list' : ''}
          </div>
        ) : null}

        {initialLoading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : sessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiPhoneLine className="size-10 opacity-30" />
            <p className="text-sm">No calls in progress on this gateway</p>
            <p className="text-xs">
              New calls appear here automatically. Finished calls are in{' '}
              <Link to="/sessions" search={{}} className="underline">
                Call Sessions
              </Link>
              .
            </p>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={visible}
            emptyMessage="No live session matches the filter."
          />
        )}
      </div>

      <HangupDialog
        target={hangupTarget}
        onClose={() => setHangupTarget(null)}
        onDone={handleReload}
      />
      <DtmfDialog target={dtmfTarget} onClose={() => setDtmfTarget(null)} />
    </div>
  )
}
