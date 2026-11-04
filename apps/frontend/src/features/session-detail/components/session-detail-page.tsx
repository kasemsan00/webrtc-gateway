import {
  RiArrowLeftLine,
  RiArrowRightLine,
  RiFileTextLine,
  RiListCheck,
  RiLoader4Line,
  RiRefreshLine,
  RiTimeLine,
} from '@remixicon/react'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type {
  SessionDialog,
  SessionEvent,
  SessionOverview,
  SessionPayload,
  SessionStats,
} from '@/features/session-detail/types'
import type { CallTarget } from '@/features/active-sessions/components/call-control-dialogs'
import type { Insight } from '@/features/session-detail/insights'
import type { SessionDetailTab } from '@/lib/search-params'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { CopyButton, IdCell, copyText } from '@/components/ui/table-cells'
import {
  DtmfDialog,
  HangupDialog,
} from '@/features/active-sessions/components/call-control-dialogs'
import { ClientDiagnosticsTab } from '@/features/session-detail/components/client-diagnostics-tab'
import {
  eventDirection,
  eventSeverity,
  formatOffset,
  humanizeEventName,
  interpretStats,
} from '@/features/session-detail/insights'
import {
  fetchPayload,
  fetchSessionDialogs,
  fetchSessionEvents,
  fetchSessionOverview,
  fetchSessionPayloads,
  fetchSessionStats,
} from '@/features/session-detail/services/session-detail-api'
import {
  EndReasonBadge,
  StateBadge,
} from '@/features/session-history/components/session-history-page'
import { TONE_CLASSES } from '@/features/gateway-status/health'
import {
  formatDurationSec,
  formatPreciseDateTime,
  formatRelativeTime,
  formatThaiDateTime,
  secondsBetween,
} from '@/lib/date-time'
import { trunkSearchFrom } from '@/lib/operations-navigation'
import { SESSION_DETAIL_TABS } from '@/lib/search-params'
import { useNow } from '@/lib/use-now'
import { usePagedList } from '@/lib/use-paged-list'
import { cn } from '@/lib/utils'

type EventQuickFilter =
  | 'all'
  | 'sip'
  | 'client'
  | 'terminal'
  | 'busy486'
  | 'noAnswer480'

const DEFAULT_PAGE_SIZE = 50
const MAX_EVIDENCE_CHARS = 16_000

const TAB_LABEL: Record<SessionDetailTab, string> = {
  overview: 'Overview',
  events: 'SIP timeline',
  payloads: 'Payloads',
  dialogs: 'Dialogs',
  stats: 'Media stats',
  'client-diagnostics': 'Client diagnostics',
}

const TAB_HINT: Record<SessionDetailTab, string> = {
  overview: 'Call parties, trunk, negotiated media and timing',
  events: 'Every SIP / client event in order, with offsets from call start',
  payloads: 'Raw SIP messages and SDP bodies captured for this call',
  dialogs: 'SIP dialog state (tags, CSeq, route set) over time',
  stats: 'RTCP and keyframe (PLI) counters sampled during the call',
  'client-diagnostics':
    'Events uploaded by the mobile/desktop client for this call',
}

export function formatEvidence(value: unknown) {
  const text =
    typeof value === 'string' ? value : JSON.stringify(value, null, 2)
  if (!text) return '(empty)'
  return text.length > MAX_EVIDENCE_CHARS
    ? `${text.slice(0, MAX_EVIDENCE_CHARS)}\n… preview truncated`
    : text
}

function copyEvidence(value: unknown, label = 'Copied') {
  void copyText(formatEvidence(value), label)
}

function downloadBinaryPayload(payload: SessionPayload) {
  if (!payload.bodyBytesB64) return
  const decoded = atob(payload.bodyBytesB64)
  const bytes = Uint8Array.from(decoded, (character) => character.charCodeAt(0))
  const url = URL.createObjectURL(
    new Blob([bytes], {
      type: payload.contentType || 'application/octet-stream',
    }),
  )
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `payload-${payload.payloadId}`
  anchor.click()
  URL.revokeObjectURL(url)
}

const formatTimestamp = (iso: string) => formatPreciseDateTime(iso)

function eventQuickFilterParams(filter: EventQuickFilter) {
  switch (filter) {
    case 'sip':
      return { category: 'sip' }
    case 'client':
      return { category: 'client' }
    case 'terminal':
      return { category: 'sip', name: 'sip_terminal_action' }
    case 'busy486':
      return { category: 'sip', sipStatusCode: 486 }
    case 'noAnswer480':
      return { category: 'sip', sipStatusCode: 480 }
    default:
      return {}
  }
}

function SIPStatusBadge({ statusCode }: { statusCode?: number }) {
  if (!statusCode) {
    return <span className="text-xs text-muted-foreground">-</span>
  }
  const variant =
    statusCode >= 200 && statusCode < 300
      ? 'success'
      : statusCode === 401 || statusCode === 407
        ? 'secondary'
        : statusCode >= 400
          ? 'destructive'
          : 'secondary'
  return (
    <Badge variant={variant} className="text-[10px]">
      {statusCode}
    </Badge>
  )
}

function LoadingState() {
  return (
    <div className="flex items-center justify-center py-20">
      <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
    </div>
  )
}

function ErrorBanner({ error }: { error: string }) {
  return (
    <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
      {error}
    </div>
  )
}

function TabToolbar({
  onRefresh,
  refreshing,
  children,
}: {
  onRefresh: () => void
  refreshing: boolean
  children?: React.ReactNode
}) {
  return (
    <div className="mb-2 flex flex-wrap items-center gap-2">
      <Button
        size="sm"
        variant="outline"
        className="h-7 gap-1 px-2 text-xs"
        onClick={onRefresh}
        disabled={refreshing}
      >
        <RiRefreshLine className={cn('size-3', refreshing && 'animate-spin')} />
        Refresh
      </Button>
      {children}
    </div>
  )
}

// ── Page ────────────────────────────────────────────────────────────────────

export function SessionDetailPage() {
  const { sessionId } = useParams({ from: '/sessions/$sessionId' })
  const search = useSearch({ from: '/sessions/$sessionId' })
  const navigate = useNavigate()
  const tab: SessionDetailTab = search.tab ?? 'overview'
  const [overview, setOverview] = useState<SessionOverview | null>(null)
  const [overviewError, setOverviewError] = useState<string | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [hangupTarget, setHangupTarget] = useState<CallTarget | null>(null)
  const [dtmfTarget, setDtmfTarget] = useState<CallTarget | null>(null)

  const loadOverview = useCallback(async () => {
    try {
      setOverview(await fetchSessionOverview(sessionId))
      setOverviewError(null)
    } catch (err) {
      setOverviewError(
        err instanceof Error ? err.message : 'Failed to fetch session overview',
      )
    } finally {
      setOverviewLoading(false)
    }
  }, [sessionId])

  useEffect(() => {
    setOverviewLoading(true)
    void loadOverview()
  }, [loadOverview])

  const isLive = Boolean(overview?.liveState) && !overview?.endedAt

  // Live calls: refresh the summary every 5s so state and duration stay current.
  useEffect(() => {
    if (!isLive) return
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') void loadOverview()
    }, 5000)
    return () => clearInterval(timer)
  }, [isLive, loadOverview])

  const setTab = (next: SessionDetailTab) => {
    void navigate({
      to: '/sessions/$sessionId',
      params: { sessionId },
      search: next === 'overview' ? {} : { tab: next },
      replace: true,
    })
  }

  const callTarget: CallTarget = {
    sessionId,
    label: overview
      ? `${overview.fromUri || '?'} → ${overview.toUri || '?'}`
      : undefined,
  }

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header
        titleSuffix={
          <span className="hidden min-w-0 items-center sm:inline-flex">
            <IdCell
              value={sessionId}
              label="Session ID"
              maxWidth="max-w-[260px]"
            />
          </span>
        }
      >
        {isLive ? (
          <>
            <Button
              size="sm"
              variant="outline"
              className="h-7 px-2 text-xs"
              onClick={() => setDtmfTarget(callTarget)}
            >
              DTMF
            </Button>
            <Button
              size="sm"
              variant="destructive"
              className="h-7 px-2 text-xs"
              onClick={() => setHangupTarget(callTarget)}
            >
              Hang up
            </Button>
          </>
        ) : null}
        <Button
          asChild
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
        >
          <Link to="/logs" search={{ q: sessionId }}>
            <RiFileTextLine className="size-3.5" />
            <span className="hidden sm:inline">Gateway logs</span>
          </Link>
        </Button>
      </Header>

      <SessionSummaryBar
        overview={overview}
        loading={overviewLoading}
        error={overviewError}
        isLive={isLive}
      />

      <div
        className="flex gap-0.5 overflow-x-auto border-b border-border px-4 pt-2"
        role="tablist"
        aria-label="Session detail sections"
      >
        {SESSION_DETAIL_TABS.map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            title={TAB_HINT[t]}
            onClick={() => setTab(t)}
            className={cn(
              'shrink-0 rounded-t-md px-3 py-1.5 text-xs font-medium transition-colors',
              tab === t
                ? 'border-b-2 border-cyan-500 text-cyan-600 dark:text-cyan-400'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {TAB_LABEL[t]}
          </button>
        ))}
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        <p className="mb-3 text-[11px] text-muted-foreground">
          {TAB_HINT[tab]}
        </p>
        {tab === 'overview' && (
          <OverviewTab
            overview={overview}
            loading={overviewLoading}
            error={overviewError}
            onRefresh={() => void loadOverview()}
          />
        )}
        {tab === 'events' && (
          <EventsTab sessionId={sessionId} callStart={overview?.createdAt} />
        )}
        {tab === 'payloads' && <PayloadsTab sessionId={sessionId} />}
        {tab === 'dialogs' && <DialogsTab sessionId={sessionId} />}
        {tab === 'stats' && <StatsTab sessionId={sessionId} />}
        {tab === 'client-diagnostics' && (
          <ClientDiagnosticsTab sessionId={sessionId} />
        )}
      </div>

      <HangupDialog
        target={hangupTarget}
        onClose={() => setHangupTarget(null)}
        onDone={() => void loadOverview()}
      />
      <DtmfDialog target={dtmfTarget} onClose={() => setDtmfTarget(null)} />
    </div>
  )
}

// ── Summary bar (visible on every tab) ──────────────────────────────────────

function sessionDurationSec(overview: SessionOverview, now: number) {
  if (overview.endedAt)
    return secondsBetween(overview.createdAt, overview.endedAt)
  if (overview.liveDurationSec !== undefined) return overview.liveDurationSec
  return secondsBetween(overview.createdAt, null, now)
}

function SessionSummaryBar({
  overview,
  loading,
  error,
  isLive,
}: {
  overview: SessionOverview | null
  loading: boolean
  error: string | null
  isLive: boolean
}) {
  const now = useNow(1000)
  if (loading && !overview) {
    return (
      <div className="border-b border-border px-4 py-2 text-xs text-muted-foreground">
        Loading call summary…
      </div>
    )
  }
  if (!overview) {
    return error ? (
      <div className="border-b border-border px-4 py-2">
        <ErrorBanner error={error} />
      </div>
    ) : null
  }
  const state = overview.liveState || overview.finalState || 'unknown'
  const duration = sessionDurationSec(overview, now)
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-b border-border bg-muted/20 px-4 py-2 text-xs">
      <span className="flex items-center gap-1.5">
        {isLive ? (
          <span className="relative flex size-2">
            <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-500 opacity-60" />
            <span className="relative inline-flex size-2 rounded-full bg-emerald-500" />
          </span>
        ) : null}
        <StateBadge state={state} />
        {overview.endReason ? (
          <EndReasonBadge reason={overview.endReason} />
        ) : null}
      </span>
      <span
        className="min-w-0 max-w-[420px] truncate"
        title={`${overview.fromUri} → ${overview.toUri}`}
      >
        <span className="mr-1 text-[10px] uppercase text-muted-foreground">
          {overview.direction || ''}
        </span>
        {overview.fromUri || '-'}{' '}
        <span className="text-muted-foreground">→</span> {overview.toUri || '-'}
      </span>
      <span>
        <span className="text-muted-foreground">Duration </span>
        <span className="font-mono tabular-nums">
          {duration === null ? '-' : formatDurationSec(duration)}
        </span>
        {isLive ? <span className="text-muted-foreground"> (live)</span> : null}
      </span>
      <span>
        <span className="text-muted-foreground">Trunk </span>
        {overview.trunkId ? (
          <Link
            to="/trunks"
            search={trunkSearchFrom({ trunkId: overview.trunkId })}
            className="text-cyan-700 hover:underline dark:text-cyan-400"
          >
            {overview.trunkName || `#${overview.trunkId}`}
          </Link>
        ) : (
          overview.trunkName || '-'
        )}
      </span>
      <span title={formatThaiDateTime(overview.createdAt)}>
        <span className="text-muted-foreground">Started </span>
        {overview.createdAt ? formatRelativeTime(overview.createdAt, now) : '-'}
      </span>
      {overview.sipCallId ? (
        <span className="flex min-w-0 items-center gap-1">
          <span className="text-muted-foreground">Call-ID</span>
          <IdCell
            value={overview.sipCallId}
            label="SIP Call-ID"
            maxWidth="max-w-[160px]"
          />
          <Link
            to="/logs"
            search={{ q: overview.sipCallId }}
            className="text-[11px] text-cyan-700 hover:underline dark:text-cyan-400"
          >
            logs
          </Link>
        </span>
      ) : null}
    </div>
  )
}

// ── Overview ────────────────────────────────────────────────────────────────

type Field = [label: string, value: React.ReactNode, hint?: string]

function FieldGroup({
  title,
  fields,
}: {
  title: string
  fields: Array<Field>
}) {
  return (
    <section className="rounded-lg border border-border">
      <h3 className="border-b border-border px-3 py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </h3>
      <dl className="divide-y divide-border/60">
        {fields.map(([label, value, hint]) => (
          <div
            key={label}
            className="grid grid-cols-[140px_1fr] gap-2 px-3 py-1.5 text-xs"
            title={hint}
          >
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-all font-medium">
              {value === undefined || value === null || value === '' ? (
                <span className="text-muted-foreground">-</span>
              ) : (
                value
              )}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function OverviewTab({
  overview,
  loading,
  error,
  onRefresh,
}: {
  overview: SessionOverview | null
  loading: boolean
  error: string | null
  onRefresh: () => void
}) {
  if (loading && !overview) return <LoadingState />
  if (!overview) return error ? <ErrorBanner error={error} /> : null

  const ports = (rtp?: number, rtcp?: number) =>
    rtp ? `${rtp} / ${rtcp || '-'}` : undefined

  return (
    <div className="space-y-3">
      <TabToolbar onRefresh={onRefresh} refreshing={loading} />
      {error ? (
        <ErrorBanner error={`${error} (showing last loaded data)`} />
      ) : null}
      <div className="grid gap-3 xl:grid-cols-2">
        <FieldGroup
          title="Call"
          fields={[
            ['From', overview.fromUri],
            ['To', overview.toUri],
            ['Direction', overview.direction],
            [
              'State',
              overview.liveState
                ? `${overview.liveState} (live)`
                : overview.finalState,
              'Live state while the call runs; final state once it ended',
            ],
            [
              'Outcome',
              overview.endReason ? (
                <EndReasonBadge reason={overview.endReason} />
              ) : undefined,
            ],
          ]}
        />
        <FieldGroup
          title="SIP"
          fields={[
            [
              'SIP Call-ID',
              overview.sipCallId ? (
                <span className="inline-flex items-center gap-1 font-mono">
                  {overview.sipCallId}
                  <CopyButton value={overview.sipCallId} label="SIP Call-ID" />
                </span>
              ) : undefined,
            ],
            [
              'Trunk',
              overview.trunkId ? (
                <Link
                  to="/trunks"
                  search={trunkSearchFrom({ trunkId: overview.trunkId })}
                  className="text-cyan-700 hover:underline dark:text-cyan-400"
                >
                  {overview.trunkName || `#${overview.trunkId}`}
                </Link>
              ) : (
                overview.trunkName
              ),
            ],
            ['SIP username', overview.sipUsername],
            [
              'Auth mode',
              overview.authMode,
              'How the WebRTC client authenticated to use this SIP identity',
            ],
          ]}
        />
        <FieldGroup
          title="Media"
          fields={[
            ['Audio profile', overview.audioProfile],
            ['Video profile', overview.videoProfile],
            [
              'Opus payload type',
              overview.sipOpusPt,
              'RTP payload type negotiated for Opus on the SIP side (audio is passed through, not transcoded)',
            ],
            [
              'Audio RTP / RTCP port',
              ports(overview.rtpAudioPort, overview.rtcpAudioPort),
            ],
            [
              'Video RTP / RTCP port',
              ports(overview.rtpVideoPort, overview.rtcpVideoPort),
            ],
            [
              'Video rejected',
              overview.videoRejected ? (
                <Badge variant="warning" className="text-[10px]">
                  yes: audio-only call
                </Badge>
              ) : (
                'no'
              ),
              'True when the far end refused the H.264 video stream',
            ],
          ]}
        />
        <FieldGroup
          title="Timing (Asia/Bangkok)"
          fields={[
            [
              'Created',
              overview.createdAt && formatTimestamp(overview.createdAt),
            ],
            [
              'Last updated',
              overview.updatedAt && formatTimestamp(overview.updatedAt),
            ],
            ['Ended', overview.endedAt && formatTimestamp(overview.endedAt)],
            [
              'Duration',
              (() => {
                const d = sessionDurationSec(overview, Date.now())
                return d === null ? undefined : formatDurationSec(d)
              })(),
              'From creation to end (includes ringing)',
            ],
          ]}
        />
      </div>
    </div>
  )
}

// ── Events / SIP timeline ───────────────────────────────────────────────────

const SEVERITY_ROW: Record<ReturnType<typeof eventSeverity>, string> = {
  error: 'bg-rose-500/10',
  warning: 'bg-amber-500/5',
  success: '',
  info: '',
}

const SEVERITY_DOT: Record<ReturnType<typeof eventSeverity>, string> = {
  error: 'bg-rose-500',
  warning: 'bg-amber-500',
  success: 'bg-emerald-500',
  info: 'bg-sky-500',
}

function EventsTab({
  sessionId,
  callStart,
}: {
  sessionId: string
  callStart?: string
}) {
  const [quickFilter, setQuickFilter] = useState<EventQuickFilter>('all')
  const [view, setView] = useState<'timeline' | 'table'>('timeline')
  const [viewEvent, setViewEvent] = useState<SessionEvent | null>(null)
  const list = usePagedList<SessionEvent>(
    (page, pageSize) =>
      fetchSessionEvents(sessionId, {
        page,
        pageSize,
        ...eventQuickFilterParams(quickFilter),
      }),
    [sessionId, quickFilter],
    DEFAULT_PAGE_SIZE,
  )
  const base = callStart ?? list.items.at(0)?.timestamp
  const errorCount = list.items.filter(
    (e) => eventSeverity(e) === 'error',
  ).length

  const columns = useMemo<Array<ColumnDef<SessionEvent>>>(
    () => [
      {
        accessorKey: 'timestamp',
        header: 'Time',
        cell: ({ row }) => (
          <span
            className="whitespace-nowrap font-mono text-[10px] text-muted-foreground"
            title={formatTimestamp(row.original.timestamp)}
          >
            {formatOffset(row.original.timestamp, base) ||
              formatTimestamp(row.original.timestamp)}
          </span>
        ),
      },
      {
        accessorKey: 'category',
        header: 'Category',
        cell: ({ row }) => (
          <Badge variant="outline" className="text-[10px]">
            {row.original.category}
          </Badge>
        ),
      },
      {
        accessorKey: 'name',
        header: 'Event',
        cell: ({ row }) => (
          <span className="text-xs" title={row.original.name}>
            {humanizeEventName(row.original.name)}
          </span>
        ),
      },
      {
        accessorKey: 'state',
        header: 'State',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.state || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'sipMethod',
        header: 'Method',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.sipMethod || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'sipStatusCode',
        header: 'Status',
        cell: ({ row }) => (
          <SIPStatusBadge statusCode={row.original.sipStatusCode} />
        ),
      },
      {
        accessorKey: 'payloadId',
        header: 'Payload',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.payloadId ? `#${row.original.payloadId}` : '-'}
          </span>
        ),
      },
      {
        id: 'details',
        header: 'Data',
        cell: ({ row }) =>
          row.original.data && Object.keys(row.original.data).length > 0 ? (
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => setViewEvent(row.original)}
            >
              View
            </Button>
          ) : (
            <span className="text-xs text-muted-foreground">-</span>
          ),
      },
    ],
    [base],
  )

  if (list.loading) return <LoadingState />

  return (
    <>
      <TabToolbar
        onRefresh={() => void list.reload()}
        refreshing={list.refreshing}
      >
        <div
          className="flex items-center gap-0.5"
          role="group"
          aria-label="View"
        >
          <Button
            size="sm"
            variant={view === 'timeline' ? 'secondary' : 'ghost'}
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setView('timeline')}
          >
            <RiTimeLine className="size-3.5" />
            Timeline
          </Button>
          <Button
            size="sm"
            variant={view === 'table' ? 'secondary' : 'ghost'}
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setView('table')}
          >
            <RiListCheck className="size-3.5" />
            Table
          </Button>
        </div>
        <span className="h-4 w-px bg-border" />
        {(
          [
            ['all', 'All'],
            ['sip', 'SIP'],
            ['client', 'Client'],
            ['terminal', 'Final outcome'],
            ['busy486', '486 busy'],
            ['noAnswer480', '480 no answer'],
          ] as const
        ).map(([value, label]) => (
          <Button
            key={value}
            size="sm"
            variant={quickFilter === value ? 'secondary' : 'ghost'}
            className="h-7 px-2 text-xs"
            onClick={() => setQuickFilter(value)}
          >
            {label}
          </Button>
        ))}
        {errorCount ? (
          <Badge variant="destructive" className="ml-auto text-[10px]">
            {errorCount} error{errorCount === 1 ? '' : 's'} on this page
          </Badge>
        ) : null}
      </TabToolbar>
      {list.error ? <ErrorBanner error={list.error} /> : null}

      {view === 'timeline' ? (
        <EventTimeline events={list.items} base={base} onView={setViewEvent} />
      ) : (
        <DataTable
          columns={columns}
          data={list.items}
          emptyMessage="No events recorded for this filter."
          getRowClassName={(e) => SEVERITY_ROW[eventSeverity(e)]}
        />
      )}
      <ServerPaginationControls
        page={list.page}
        totalPages={list.totalPages}
        total={list.total}
        pageSize={list.pageSize}
        onPageChange={list.setPage}
        onPageSizeChange={list.setPageSize}
        totalLabel="events"
      />
      <Dialog
        open={!!viewEvent}
        onOpenChange={(open) => {
          if (!open) setViewEvent(null)
        }}
      >
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>
              {viewEvent ? humanizeEventName(viewEvent.name) : 'Event'}
            </DialogTitle>
            <DialogDescription className="break-all font-mono text-xs">
              {viewEvent?.name} ·{' '}
              {viewEvent ? formatTimestamp(viewEvent.timestamp) : ''} ·{' '}
              {viewEvent?.sipCallId || 'no SIP Call-ID'}
            </DialogDescription>
          </DialogHeader>
          <Button
            size="sm"
            variant="outline"
            className="w-fit"
            onClick={() => copyEvidence(viewEvent?.data, 'Event JSON copied')}
          >
            Copy JSON
          </Button>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {formatEvidence(viewEvent?.data)}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  )
}

function EventTimeline({
  events,
  base,
  onView,
}: {
  events: Array<SessionEvent>
  base?: string
  onView: (event: SessionEvent) => void
}) {
  if (events.length === 0) {
    return (
      <p className="rounded-md border border-dashed border-border py-10 text-center text-xs text-muted-foreground">
        No events recorded for this filter.
      </p>
    )
  }
  return (
    <ol className="relative space-y-0.5 rounded-md border border-border p-2">
      <li className="grid grid-cols-[80px_24px_1fr] gap-2 px-2 pb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
        <span>Offset</span>
        <span />
        <span>
          Event · <RiArrowRightLine className="inline size-3" /> sent by gateway
          · <RiArrowLeftLine className="inline size-3" /> received from far end
        </span>
      </li>
      {events.map((event) => {
        const severity = eventSeverity(event)
        const direction = eventDirection(event.name)
        const hasData = event.data && Object.keys(event.data).length > 0
        return (
          <li
            key={event.id}
            className={cn(
              'grid grid-cols-[80px_24px_1fr] items-center gap-2 rounded px-2 py-1 text-xs',
              SEVERITY_ROW[severity],
            )}
          >
            <span
              className="font-mono text-[10px] text-muted-foreground"
              title={formatTimestamp(event.timestamp)}
            >
              {formatOffset(event.timestamp, base)}
            </span>
            <span className="flex items-center justify-center">
              {direction === 'out' ? (
                <RiArrowRightLine className="size-3.5 text-cyan-600 dark:text-cyan-400" />
              ) : direction === 'in' ? (
                <RiArrowLeftLine className="size-3.5 text-violet-600 dark:text-violet-400" />
              ) : (
                <span
                  className={cn('size-2 rounded-full', SEVERITY_DOT[severity])}
                />
              )}
            </span>
            <span className="flex min-w-0 flex-wrap items-center gap-1.5">
              <span className="font-medium" title={event.name}>
                {humanizeEventName(event.name)}
              </span>
              {event.sipStatusCode ? (
                <SIPStatusBadge statusCode={event.sipStatusCode} />
              ) : null}
              {event.state ? (
                <span className="text-[10px] text-muted-foreground">
                  state: {event.state}
                </span>
              ) : null}
              <Badge variant="outline" className="text-[9px]">
                {event.category}
              </Badge>
              {event.payloadId ? (
                <span className="text-[10px] text-muted-foreground">
                  payload #{event.payloadId}
                </span>
              ) : null}
              {hasData ? (
                <button
                  type="button"
                  className="text-[10px] text-cyan-700 hover:underline dark:text-cyan-400"
                  onClick={() => onView(event)}
                >
                  data
                </button>
              ) : null}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

// ── Payloads ────────────────────────────────────────────────────────────────

function PayloadsTab({ sessionId }: { sessionId: string }) {
  const list = usePagedList<SessionPayload>(
    (page, pageSize) => fetchSessionPayloads(sessionId, { page, pageSize }),
    [sessionId],
    DEFAULT_PAGE_SIZE,
  )
  const [viewPayload, setViewPayload] = useState<SessionPayload | null>(null)
  const [payloadLoading, setPayloadLoading] = useState(false)
  const [payloadError, setPayloadError] = useState<string | null>(null)

  const handleViewPayload = useCallback(async (payloadId: number) => {
    setPayloadLoading(true)
    setPayloadError(null)
    try {
      setViewPayload(await fetchPayload(payloadId))
    } catch (err) {
      setViewPayload(null)
      setPayloadError(
        err instanceof Error
          ? err.message
          : `Failed to load payload #${payloadId}`,
      )
    } finally {
      setPayloadLoading(false)
    }
  }, [])

  const columns = useMemo<Array<ColumnDef<SessionPayload>>>(
    () => [
      {
        accessorKey: 'payloadId',
        header: 'ID',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            #{row.original.payloadId}
          </span>
        ),
      },
      {
        accessorKey: 'timestamp',
        header: 'Timestamp',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {formatTimestamp(row.original.timestamp)}
          </span>
        ),
      },
      {
        accessorKey: 'kind',
        header: 'Kind',
        cell: ({ row }) => (
          <Badge variant="outline" className="text-[10px]">
            {row.original.kind}
          </Badge>
        ),
      },
      {
        accessorKey: 'contentType',
        header: 'Content type',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.contentType || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'bodyText',
        header: 'First line',
        cell: ({ row }) => (
          <span
            className="inline-block max-w-[320px] truncate font-mono text-[11px] text-muted-foreground"
            title={row.original.bodyText?.slice(0, 400)}
          >
            {row.original.bodyText?.split(/\r?\n/, 1)[0] || '-'}
          </span>
        ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        cell: ({ row }) => (
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="secondary"
              className="h-6 px-2 text-[10px]"
              onClick={() => void handleViewPayload(row.original.payloadId)}
              disabled={payloadLoading}
            >
              View
            </Button>
          </div>
        ),
      },
    ],
    [handleViewPayload, payloadLoading],
  )

  if (list.loading) return <LoadingState />

  return (
    <>
      <TabToolbar
        onRefresh={() => void list.reload()}
        refreshing={list.refreshing}
      />
      {list.error ? <ErrorBanner error={list.error} /> : null}
      {payloadError ? <ErrorBanner error={payloadError} /> : null}
      <DataTable
        columns={columns}
        data={list.items}
        emptyMessage="No payloads captured. Payload capture may be disabled in config."
      />
      <ServerPaginationControls
        page={list.page}
        totalPages={list.totalPages}
        total={list.total}
        pageSize={list.pageSize}
        onPageChange={list.setPage}
        onPageSizeChange={list.setPageSize}
        totalLabel="payloads"
      />

      <Dialog
        open={!!viewPayload}
        onOpenChange={(open) => {
          if (!open) setViewPayload(null)
        }}
      >
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>
              Payload #{viewPayload?.payloadId} — {viewPayload?.kind}
            </DialogTitle>
            <DialogDescription>
              {viewPayload?.contentType} |{' '}
              {formatTimestamp(viewPayload?.timestamp ?? '')}
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {formatEvidence(viewPayload?.bodyText)}
          </pre>
          <Button
            size="sm"
            variant="outline"
            className="w-fit"
            onClick={() =>
              copyEvidence(viewPayload?.bodyText, 'Raw payload copied')
            }
          >
            Copy raw
          </Button>
          <div className="space-y-2 text-xs">
            <div>
              <span className="text-muted-foreground">Binary payload: </span>
              {viewPayload?.bodyBytesB64
                ? `${viewPayload.bodyBytesB64.length} base64 characters (not rendered)`
                : '-'}
            </div>
            {viewPayload?.bodyBytesB64 ? (
              <Button
                size="sm"
                variant="outline"
                className="w-fit"
                onClick={() => downloadBinaryPayload(viewPayload)}
              >
                Download binary
              </Button>
            ) : null}
            <div className="font-medium">Parsed</div>
            <Button
              size="sm"
              variant="outline"
              className="w-fit"
              onClick={() =>
                copyEvidence(viewPayload?.parsed, 'Parsed payload copied')
              }
            >
              Copy parsed
            </Button>
            <pre className="max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs">
              {formatEvidence(viewPayload?.parsed)}
            </pre>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

// ── Dialogs ─────────────────────────────────────────────────────────────────

function DialogsTab({ sessionId }: { sessionId: string }) {
  const list = usePagedList<SessionDialog>(
    (page, pageSize) => fetchSessionDialogs(sessionId, { page, pageSize }),
    [sessionId],
    DEFAULT_PAGE_SIZE,
  )

  const columns = useMemo<Array<ColumnDef<SessionDialog>>>(
    () => [
      {
        accessorKey: 'timestamp',
        header: 'Timestamp',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {formatTimestamp(row.original.timestamp)}
          </span>
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
        accessorKey: 'fromTag',
        header: 'From tag',
        cell: ({ row }) => (
          <IdCell
            value={row.original.fromTag}
            label="From tag"
            maxWidth="max-w-[90px]"
          />
        ),
      },
      {
        accessorKey: 'toTag',
        header: 'To tag',
        cell: ({ row }) => (
          <IdCell
            value={row.original.toTag}
            label="To tag"
            maxWidth="max-w-[90px]"
          />
        ),
      },
      {
        accessorKey: 'remoteContact',
        header: 'Remote contact',
        cell: ({ row }) => (
          <span
            className="inline-block max-w-[200px] truncate font-mono text-[11px] text-muted-foreground"
            title={row.original.remoteContact}
          >
            {row.original.remoteContact || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'cseq',
        header: 'CSeq',
        cell: ({ row }) => <span className="text-xs">{row.original.cseq}</span>,
      },
      {
        accessorKey: 'routeSet',
        header: 'Route set',
        cell: ({ row }) => (
          <span
            className="inline-block max-w-[200px] truncate text-[10px] text-muted-foreground"
            title={row.original.routeSet?.join('\n')}
          >
            {row.original.routeSet?.length
              ? row.original.routeSet.join(', ')
              : '-'}
          </span>
        ),
      },
    ],
    [],
  )

  if (list.loading) return <LoadingState />

  return (
    <>
      <TabToolbar
        onRefresh={() => void list.reload()}
        refreshing={list.refreshing}
      />
      {list.error ? <ErrorBanner error={list.error} /> : null}
      <DataTable
        columns={columns}
        data={list.items}
        emptyMessage="No SIP dialog snapshots recorded."
      />
      <ServerPaginationControls
        page={list.page}
        totalPages={list.totalPages}
        total={list.total}
        pageSize={list.pageSize}
        onPageChange={list.setPage}
        onPageSizeChange={list.setPageSize}
        totalLabel="snapshots"
      />
    </>
  )
}

// ── Stats ───────────────────────────────────────────────────────────────────

function InsightList({ insights }: { insights: Array<Insight> }) {
  return (
    <div className="mb-3 grid gap-2 md:grid-cols-2">
      {insights.map((insight) => {
        const tone = TONE_CLASSES[insight.tone]
        return (
          <div
            key={insight.title}
            className={cn(
              'rounded-md border px-3 py-2 text-xs',
              tone.border,
              tone.bg,
            )}
          >
            <p
              className={cn('flex items-center gap-1.5 font-medium', tone.text)}
            >
              <span className={cn('size-2 rounded-full', tone.dot)} />
              {insight.title}
            </p>
            {insight.detail ? (
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                {insight.detail}
              </p>
            ) : null}
          </div>
        )
      })}
    </div>
  )
}

function StatsTab({ sessionId }: { sessionId: string }) {
  const list = usePagedList<SessionStats>(
    (page, pageSize) => fetchSessionStats(sessionId, { page, pageSize }),
    [sessionId],
    DEFAULT_PAGE_SIZE,
  )
  const [viewData, setViewData] = useState<Record<string, unknown> | null>(null)
  const insights = useMemo(() => interpretStats(list.items), [list.items])

  const header = (label: string, hint: string) => () => (
    <span
      title={hint}
      className="cursor-help underline decoration-dotted underline-offset-2"
    >
      {label}
    </span>
  )

  const columns = useMemo<Array<ColumnDef<SessionStats>>>(
    () => [
      {
        accessorKey: 'timestamp',
        header: 'Sampled at',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {formatTimestamp(row.original.timestamp)}
          </span>
        ),
      },
      {
        accessorKey: 'audioRtcpRr',
        header: header(
          'Audio RR',
          'RTCP Receiver Reports received for audio (far end tells us what it received)',
        ),
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {row.original.audioRtcpRr}
          </span>
        ),
      },
      {
        accessorKey: 'audioRtcpSr',
        header: header('Audio SR', 'RTCP Sender Reports received for audio'),
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {row.original.audioRtcpSr}
          </span>
        ),
      },
      {
        accessorKey: 'videoRtcpRr',
        header: header('Video RR', 'RTCP Receiver Reports received for video'),
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {row.original.videoRtcpRr}
          </span>
        ),
      },
      {
        accessorKey: 'videoRtcpSr',
        header: header('Video SR', 'RTCP Sender Reports received for video'),
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {row.original.videoRtcpSr}
          </span>
        ),
      },
      {
        accessorKey: 'pliSent',
        header: header(
          'PLI sent',
          'Picture Loss Indications: keyframe requests the gateway sent',
        ),
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">{row.original.pliSent}</span>
        ),
      },
      {
        accessorKey: 'pliResponse',
        header: header('PLI answered', 'Keyframes that arrived after a PLI'),
        cell: ({ row }) => (
          <span
            className={cn(
              'text-xs tabular-nums',
              row.original.pliSent > 0 &&
                row.original.pliResponse === 0 &&
                'font-semibold text-rose-600 dark:text-rose-400',
            )}
          >
            {row.original.pliResponse}
          </span>
        ),
      },
      {
        accessorKey: 'lastKeyframeAt',
        header: 'Last keyframe',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.lastKeyframeAt
              ? formatTimestamp(row.original.lastKeyframeAt)
              : '-'}
          </span>
        ),
      },
      {
        accessorKey: 'lastPliSentAt',
        header: 'Last PLI',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.lastPliSentAt
              ? formatTimestamp(row.original.lastPliSentAt)
              : '-'}
          </span>
        ),
      },
      {
        id: 'details',
        header: 'Raw',
        cell: ({ row }) =>
          row.original.data && Object.keys(row.original.data).length > 0 ? (
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => setViewData(row.original.data || null)}
            >
              View
            </Button>
          ) : (
            <span className="text-xs text-muted-foreground">-</span>
          ),
      },
    ],
    [],
  )

  if (list.loading) return <LoadingState />

  return (
    <>
      <TabToolbar
        onRefresh={() => void list.reload()}
        refreshing={list.refreshing}
      />
      {list.error ? <ErrorBanner error={list.error} /> : null}
      <InsightList insights={insights} />
      <DataTable
        columns={columns}
        data={list.items}
        emptyMessage="No media stats sampled."
      />
      <ServerPaginationControls
        page={list.page}
        totalPages={list.totalPages}
        total={list.total}
        pageSize={list.pageSize}
        onPageChange={list.setPage}
        onPageSizeChange={list.setPageSize}
        totalLabel="samples"
      />
      <Dialog
        open={!!viewData}
        onOpenChange={(open) => {
          if (!open) setViewData(null)
        }}
      >
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Raw stats sample</DialogTitle>
          </DialogHeader>
          <Button
            size="sm"
            variant="outline"
            className="w-fit"
            onClick={() => copyEvidence(viewData, 'Stats JSON copied')}
          >
            Copy JSON
          </Button>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {formatEvidence(viewData)}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  )
}
