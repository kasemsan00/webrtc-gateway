// apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx
import {
  RiLoader4Line,
  RiMore2Fill,
  RiRefreshLine,
  RiRouterLine,
} from '@remixicon/react'
import { Link } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  fetchWSClients,
  subscribeWSClientEvents,
} from '../services/ws-clients-api'
import { resolveWSClientPresenceLabel } from '../types'
import type {
  WSClient,
  WSClientPresenceLabel,
  WSClientStreamEvent,
} from '../types'
import type { ColumnDef } from '@tanstack/react-table'
import type { CallTarget } from '@/features/active-sessions/components/call-control-dialogs'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
import { formatDurationSec, secondsBetween } from '@/lib/date-time'
import { trunkSearchFrom } from '@/lib/operations-navigation'
import { useNow } from '@/lib/use-now'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'

const ACTIVE_CALL_STATES = new Set(['incall', 'connecting', 'ringing'])

type ModeFilter = 'all' | WSClientPresenceLabel

const MODE_HELP: Record<WSClientPresenceLabel, string> = {
  agent: 'Desktop agent console (ephemeral presence; may hold several trunks).',
  mobile: 'Mobile app with sticky presence: reachable by push when offline.',
  public: 'Public / guest caller using a shared public account.',
}

/** Applies one SSE event to the client list (exported for tests). */
export function applyWSClientEvent(
  prev: Array<WSClient>,
  event: WSClientStreamEvent,
): Array<WSClient> {
  if (event.type === 'disconnected') {
    return prev.filter((c) => c.clientId !== event.clientId)
  }
  if (event.client) {
    const idx = prev.findIndex((c) => c.clientId === event.clientId)
    if (idx === -1) return [...prev, event.client]
    const next = [...prev]
    next[idx] = event.client
    return next
  }
  return prev
}

function matchesSearch(client: WSClient, query: string) {
  if (!query) return true
  const q = query.toLowerCase()
  return [
    client.clientId,
    client.sessionId,
    client.sipUsername,
    client.sipDomain,
    client.authSubject,
    client.resolvedTrunkPublicId,
    client.callState,
  ].some((value) => value && value.toLowerCase().includes(q))
}

export function WSClientsPage() {
  const [clients, setClients] = useState<Array<WSClient>>([])
  const [initialLoading, setInitialLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [mode, setMode] = useState<ModeFilter>('all')
  const [onlyInCall, setOnlyInCall] = useState(false)
  const [hangupTarget, setHangupTarget] = useState<CallTarget | null>(null)
  const [dtmfTarget, setDtmfTarget] = useState<CallTarget | null>(null)
  const now = useNow(5000)

  const load = useCallback(async () => {
    setRefreshing(true)
    try {
      const data = await fetchWSClients()
      setClients(data)
      setError(null)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Failed to fetch WS clients',
      )
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

  const handleSseEvent = useCallback((event: WSClientStreamEvent) => {
    setClients((prev) => applyWSClientEvent(prev, event))
  }, [])

  // Events patch the list in place; reconnects and polling resync the list.
  useVisibilityRealtimeReload({
    name: 'ws-clients',
    subscribe: subscribeWSClientEvents,
    onEvent: handleSseEvent,
    onReload: handleReload,
    pollIntervalMs: 30_000,
  })

  const counts = useMemo(() => {
    const byMode: Record<WSClientPresenceLabel, number> = {
      agent: 0,
      mobile: 0,
      public: 0,
    }
    let inCall = 0
    let unresolved = 0
    for (const c of clients) {
      byMode[resolveWSClientPresenceLabel(c)] += 1
      if (c.sessionId && ACTIVE_CALL_STATES.has(c.callState ?? '')) inCall += 1
      if (!c.trunkResolved) unresolved += 1
    }
    return { byMode, inCall, unresolved }
  }, [clients])

  const visible = useMemo(
    () =>
      clients
        .filter(
          (c) => mode === 'all' || resolveWSClientPresenceLabel(c) === mode,
        )
        .filter(
          (c) =>
            !onlyInCall ||
            (!!c.sessionId && ACTIVE_CALL_STATES.has(c.callState ?? '')),
        )
        .filter((c) => matchesSearch(c, query.trim()))
        .sort((a, b) => b.connectedAt.localeCompare(a.connectedAt)),
    [clients, mode, onlyInCall, query],
  )

  const columns = useMemo<Array<ColumnDef<WSClient>>>(
    () => [
      {
        accessorKey: 'clientId',
        header: 'Client',
        cell: ({ row }) => (
          <IdCell
            value={row.original.clientId}
            label="Client ID"
            maxWidth="max-w-[90px]"
          />
        ),
      },
      {
        id: 'mode',
        header: 'Type',
        cell: ({ row }) => {
          const c = row.original
          const m = resolveWSClientPresenceLabel(c)
          const variant =
            m === 'agent' ? 'warning' : m === 'mobile' ? 'success' : 'default'
          return (
            <div className="flex flex-col gap-0.5" title={MODE_HELP[m]}>
              <Badge variant={variant} className="w-fit text-[10px]">
                {m}
              </Badge>
              {m === 'agent' && (c.agentTrunkRefCount ?? 0) > 0 ? (
                <span className="font-mono text-[10px] text-muted-foreground">
                  {c.agentTrunkRefCount} WS
                </span>
              ) : null}
            </div>
          )
        },
      },
      {
        id: 'sip',
        header: 'SIP account',
        cell: ({ row }) => {
          const c = row.original
          const account = c.sipUsername
            ? `${c.sipUsername}@${c.sipDomain ?? '?'}${c.sipPort ? `:${c.sipPort}` : ''}`
            : '-'
          return (
            <span
              className="inline-block max-w-[220px] truncate font-mono text-xs"
              title={account}
            >
              {account}
            </span>
          )
        },
      },
      {
        id: 'trunk',
        header: 'Trunk',
        cell: ({ row }) => {
          const c = row.original
          if (!c.trunkResolved) {
            return (
              <Badge
                variant="warning"
                className="text-[10px]"
                title="The client connected but no trunk matched its SIP account yet; it cannot place or receive trunk calls."
              >
                unresolved
              </Badge>
            )
          }
          const label = c.resolvedTrunkPublicId
            ? c.resolvedTrunkPublicId.slice(0, 8)
            : `#${c.resolvedTrunkId}`
          return (
            <Link
              to="/trunks"
              search={trunkSearchFrom({
                trunkPublicId: c.resolvedTrunkPublicId,
                trunkId: c.resolvedTrunkId,
              })}
              className="font-mono text-xs text-cyan-700 hover:underline dark:text-cyan-400"
              title={c.resolvedTrunkPublicId || undefined}
            >
              {label}
            </Link>
          )
        },
      },
      {
        accessorKey: 'availability',
        header: 'Availability',
        cell: ({ row }) => {
          const a = row.original.availability
          const variant =
            a === 'idle'
              ? 'success'
              : a === 'busy'
                ? 'warning'
                : a === 'unavailable'
                  ? 'destructive'
                  : 'secondary'
          return (
            <Badge variant={variant} className="text-[10px]">
              {a || '-'}
            </Badge>
          )
        },
      },
      {
        id: 'call',
        header: 'Call',
        cell: ({ row }) => {
          const c = row.original
          return (
            <div className="space-y-0.5 text-xs">
              <p>
                {c.callState || 'idle'}
                {(c.activeCalls ?? 0) > 0 ? (
                  <span className="text-muted-foreground">
                    {' '}
                    · {c.activeCalls} call{c.activeCalls === 1 ? '' : 's'}
                  </span>
                ) : null}
                {c.multiCall ? (
                  <Badge variant="outline" className="ml-1 text-[9px]">
                    multi-call
                  </Badge>
                ) : null}
              </p>
              {c.sessionId ? (
                <SessionIdLink
                  sessionId={c.sessionId}
                  maxWidth="max-w-[120px]"
                />
              ) : null}
            </div>
          )
        },
      },
      {
        accessorKey: 'connectedAt',
        header: 'Connected',
        cell: ({ row }) => {
          const seconds = secondsBetween(row.original.connectedAt, null, now)
          return (
            <div className="text-xs">
              <TimestampCell
                value={row.original.connectedAt}
                relative
                now={now}
              />
              {seconds !== null ? (
                <p className="font-mono text-[10px] text-muted-foreground">
                  up {formatDurationSec(seconds)}
                </p>
              ) : null}
            </div>
          )
        },
      },
      {
        accessorKey: 'authSubject',
        header: 'Auth subject',
        cell: ({ row }) => (
          <IdCell
            value={row.original.authSubject}
            label="Auth subject"
            maxWidth="max-w-[110px]"
          />
        ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        cell: ({ row }) => {
          const c = row.original
          const inCall =
            !!c.sessionId && ACTIVE_CALL_STATES.has(c.callState ?? '')
          const target = c.sessionId
            ? {
                sessionId: c.sessionId,
                label: c.sipUsername ? `client ${c.sipUsername}` : undefined,
              }
            : null
          return (
            <div className="flex justify-end gap-1">
              {inCall && target ? (
                <Button
                  size="sm"
                  variant="destructive"
                  className="h-6 px-2 text-[10px]"
                  onClick={() => setHangupTarget(target)}
                >
                  Hangup
                </Button>
              ) : null}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-6"
                    aria-label={`More actions for client ${c.clientId}`}
                  >
                    <RiMore2Fill className="size-3.5" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-52">
                  {inCall && target ? (
                    <DropdownMenuItem onSelect={() => setDtmfTarget(target)}>
                      Send DTMF…
                    </DropdownMenuItem>
                  ) : null}
                  <DropdownMenuItem asChild>
                    <Link to="/logs" search={{ q: c.clientId }}>
                      Logs for this client ID
                    </Link>
                  </DropdownMenuItem>
                  {c.authSubject ? (
                    <DropdownMenuItem asChild>
                      <Link
                        to="/client-diagnostics"
                        search={{ authSubject: c.authSubject }}
                      >
                        Client diagnostics for this user
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
    [now],
  )

  const modeButton = (value: ModeFilter, label: string, count: number) => (
    <Button
      key={value}
      size="sm"
      variant={mode === value ? 'secondary' : 'ghost'}
      className="h-7 gap-1 px-2 text-xs"
      onClick={() => setMode(value)}
      aria-pressed={mode === value}
      title={value === 'all' ? undefined : MODE_HELP[value]}
    >
      {label}
      <span className="tabular-nums text-muted-foreground">{count}</span>
    </Button>
  )

  const filters = (
    <div className="flex flex-wrap items-center gap-2">
      <Input
        className="h-7 w-full text-xs sm:w-56"
        placeholder="Search client, SIP user, session, subject…"
        aria-label="Search WebSocket clients"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <div
        className="flex items-center gap-0.5"
        role="group"
        aria-label="Client type"
      >
        {modeButton('all', 'All', clients.length)}
        {modeButton('mobile', 'Mobile', counts.byMode.mobile)}
        {modeButton('agent', 'Agent', counts.byMode.agent)}
        {modeButton('public', 'Public', counts.byMode.public)}
      </div>
      <Button
        size="sm"
        variant={onlyInCall ? 'secondary' : 'ghost'}
        className="h-7 gap-1 px-2 text-xs"
        onClick={() => setOnlyInCall((v) => !v)}
        aria-pressed={onlyInCall}
      >
        In a call
        <span className="tabular-nums text-muted-foreground">
          {counts.inCall}
        </span>
      </Button>
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
        <div className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            <strong className="text-foreground tabular-nums">
              {clients.length}
            </strong>{' '}
            connected
          </span>
          <span>{counts.inCall} in a call</span>
          {counts.unresolved ? (
            <span className="text-amber-600 dark:text-amber-400">
              {counts.unresolved} without a resolved trunk
            </span>
          ) : null}
          <span className="ml-auto">Connects and disconnects update live</span>
        </div>
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
            {error}
          </div>
        ) : null}
        {initialLoading ? (
          <div className="flex justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : clients.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiRouterLine className="size-10 opacity-30" />
            <p className="text-sm">
              No app or agent is connected over WebSocket
            </p>
            <p className="text-xs">
              Clients appear here as soon as they open the gateway WebSocket.
            </p>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={visible}
            emptyMessage="No connected client matches the filter."
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
