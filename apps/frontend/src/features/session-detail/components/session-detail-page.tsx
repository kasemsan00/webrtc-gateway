import {
  RiArrowLeftLine,
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { Link, useParams } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type {
  SessionDialog,
  SessionEvent,
  SessionOverview,
  SessionPayload,
  SessionStats,
} from '@/features/session-detail/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import Header from '@/components/Header'
import { formatThaiDateTime } from '@/lib/date-time'
import { useTheme } from '@/lib/theme'
import {
  fetchPayload,
  fetchSessionDialogs,
  fetchSessionEvents,
  fetchSessionOverview,
  fetchSessionPayloads,
  fetchSessionStats,
} from '@/features/session-detail/services/session-detail-api'
import { ClientDiagnosticsTab } from '@/features/session-detail/components/client-diagnostics-tab'

type TabType =
  | 'overview'
  | 'events'
  | 'payloads'
  | 'dialogs'
  | 'stats'
  | 'client-diagnostics'
type EventQuickFilter =
  | 'all'
  | 'sip'
  | 'client'
  | 'terminal'
  | 'busy486'
  | 'noAnswer480'

const DEFAULT_PAGE_SIZE = 50
const MAX_EVIDENCE_CHARS = 16_000

export function formatEvidence(value: unknown) {
  const text =
    typeof value === 'string' ? value : JSON.stringify(value, null, 2)
  if (!text) return '(empty)'
  return text.length > MAX_EVIDENCE_CHARS
    ? `${text.slice(0, MAX_EVIDENCE_CHARS)}\n… preview truncated`
    : text
}

function copyEvidence(value: unknown) {
  void navigator.clipboard.writeText(formatEvidence(value))
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

const formatTimestamp = (iso: string) =>
  formatThaiDateTime(iso, {
    fractionalSecondDigits: 3,
  })

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
  if (statusCode >= 200 && statusCode < 300) {
    return (
      <Badge variant="success" className="text-[10px]">
        {statusCode}
      </Badge>
    )
  }
  if (statusCode === 480 || statusCode === 486 || statusCode >= 500) {
    return (
      <Badge variant="destructive" className="text-[10px]">
        {statusCode}
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="text-[10px]">
      {statusCode}
    </Badge>
  )
}

export function SessionDetailPage() {
  const { theme, toggleTheme } = useTheme()
  const params = useParams({ strict: false })
  const sessionId = (params as Record<string, string>).sessionId || ''
  const [tab, setTab] = useState<TabType>('overview')

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <Link to="/sessions">
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 px-2 text-xs"
            >
              <RiArrowLeftLine className="size-3.5" />
              Back
            </Button>
          </Link>
          <Separator orientation="vertical" className="h-4" />
          <span className="font-mono text-muted-foreground">{sessionId}</span>
          <Separator orientation="vertical" className="h-4" />
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

      {/* Tabs */}
      <div className="flex gap-0.5 border-b border-border px-4 pt-2">
        {(
          [
            'overview',
            'events',
            'payloads',
            'dialogs',
            'stats',
            'client-diagnostics',
          ] as Array<TabType>
        ).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`rounded-t-md px-3 py-1.5 text-xs font-medium transition-colors ${
              tab === t
                ? 'border-b-2 border-cyan-500 text-cyan-600 dark:text-cyan-400'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {t === 'client-diagnostics'
              ? 'Client diagnostics'
              : t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto p-4">
        {tab === 'events' && <EventsTab sessionId={sessionId} />}
        {tab === 'overview' && <OverviewTab sessionId={sessionId} />}
        {tab === 'payloads' && <PayloadsTab sessionId={sessionId} />}
        {tab === 'dialogs' && <DialogsTab sessionId={sessionId} />}
        {tab === 'stats' && <StatsTab sessionId={sessionId} />}
        {tab === 'client-diagnostics' && (
          <ClientDiagnosticsTab sessionId={sessionId} />
        )}
      </div>
    </div>
  )
}

function OverviewTab({ sessionId }: { sessionId: string }) {
  const [overview, setOverview] = useState<SessionOverview | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setOverview(await fetchSessionOverview(sessionId))
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Failed to fetch session overview',
      )
    } finally {
      setLoading(false)
    }
  }, [sessionId])
  useEffect(() => {
    void load()
  }, [load])
  if (loading) return <LoadingState />
  if (error) return <ErrorBanner error={error} />
  if (!overview) return null
  const fields: Array<[string, string | number | boolean | undefined]> = [
    ['State', overview.liveState || overview.finalState],
    ['Direction', overview.direction],
    ['Outcome', overview.endReason],
    ['SIP Call-ID', overview.sipCallId],
    ['Auth mode', overview.authMode],
    ['Trunk', overview.trunkName || overview.trunkId],
    ['SIP username', overview.sipUsername],
    ['Audio profile', overview.audioProfile],
    ['Video profile', overview.videoProfile],
    ['Opus PT', overview.sipOpusPt],
    [
      'Audio RTP/RTCP',
      overview.rtpAudioPort &&
        `${overview.rtpAudioPort} / ${overview.rtcpAudioPort || '-'}`,
    ],
    [
      'Video RTP/RTCP',
      overview.rtpVideoPort &&
        `${overview.rtpVideoPort} / ${overview.rtcpVideoPort || '-'}`,
    ],
    ['Video rejected', overview.videoRejected],
    ['Created', overview.createdAt && formatTimestamp(overview.createdAt)],
    ['Updated', overview.updatedAt && formatTimestamp(overview.updatedAt)],
    ['Ended', overview.endedAt && formatTimestamp(overview.endedAt)],
    [
      'Live duration',
      overview.liveDurationSec !== undefined
        ? `${overview.liveDurationSec}s`
        : undefined,
    ],
  ]
  return (
    <div className="space-y-3">
      <Button
        size="sm"
        variant="outline"
        className="h-7 gap-1 px-2 text-xs"
        onClick={() => void load()}
      >
        <RiRefreshLine className="size-3" />
        Refresh
      </Button>
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {fields.map(([label, value]) => (
          <div
            key={label}
            className="rounded-md border border-border p-3 text-xs"
          >
            <div className="text-muted-foreground">{label}</div>
            <div className="mt-1 break-all font-medium">
              {value === undefined || value === '' ? '-' : String(value)}
            </div>
          </div>
        ))}
      </div>
    </div>
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
    <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
      {error}
    </div>
  )
}

// --- Events Tab ---
function EventsTab({ sessionId }: { sessionId: string }) {
  const [events, setEvents] = useState<Array<SessionEvent>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [quickFilter, setQuickFilter] = useState<EventQuickFilter>('all')
  const [viewEvent, setViewEvent] = useState<SessionEvent | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(
    async (p?: number) => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetchSessionEvents(sessionId, {
          page: p ?? page,
          pageSize,
          ...eventQuickFilterParams(quickFilter),
        })
        setEvents(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch events')
      } finally {
        setLoading(false)
      }
    },
    [sessionId, page, pageSize, quickFilter],
  )

  useEffect(() => {
    void load()
  }, [load])

  const columns = useMemo<Array<ColumnDef<SessionEvent>>>(
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
        header: 'Name',
        cell: ({ row }) => <span className="text-xs">{row.original.name}</span>,
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
        header: 'SIP Method',
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
        accessorKey: 'sipCallId',
        header: 'SIP Call-ID',
        cell: ({ row }) => (
          <span className="inline-block max-w-[140px] truncate font-mono text-[10px] text-muted-foreground">
            {row.original.sipCallId || '-'}
          </span>
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
        header: 'Details',
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
    [],
  )

  if (loading) return <LoadingState />
  if (error) return <ErrorBanner error={error} />

  return (
    <>
      <div className="mb-2 flex items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => load()}
        >
          <RiRefreshLine className="size-3" />
          Refresh
        </Button>
        <Separator orientation="vertical" className="h-4" />
        {[
          ['all', 'All'],
          ['sip', 'SIP'],
          ['client', 'Client'],
          ['terminal', 'Terminal'],
          ['busy486', '486'],
          ['noAnswer480', '480'],
        ].map(([value, label]) => (
          <Button
            key={value}
            size="sm"
            variant={quickFilter === value ? 'secondary' : 'ghost'}
            className="h-7 px-2 text-xs"
            onClick={() => {
              setQuickFilter(value as EventQuickFilter)
              setPage(1)
            }}
          >
            {label}
          </Button>
        ))}
      </div>
      <DataTable
        columns={columns}
        data={events}
        emptyMessage="No events found."
      />
      <ServerPaginationControls
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        onPageChange={setPage}
        onPageSizeChange={(nextPageSize) => {
          setPageSize(nextPageSize)
          setPage(1)
        }}
        totalLabel="records"
      />
      <Dialog
        open={!!viewEvent}
        onOpenChange={(open) => {
          if (!open) setViewEvent(null)
        }}
      >
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Event details</DialogTitle>
            <DialogDescription>
              {viewEvent?.name} · {viewEvent?.sipCallId || 'no SIP Call-ID'}
            </DialogDescription>
          </DialogHeader>
          <Button
            size="sm"
            variant="outline"
            className="w-fit"
            onClick={() => copyEvidence(viewEvent?.data)}
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

// --- Payloads Tab ---
function PayloadsTab({ sessionId }: { sessionId: string }) {
  const [payloads, setPayloads] = useState<Array<SessionPayload>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [viewPayload, setViewPayload] = useState<SessionPayload | null>(null)
  const [payloadLoading, setPayloadLoading] = useState(false)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(
    async (p?: number) => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetchSessionPayloads(sessionId, {
          page: p ?? page,
          pageSize,
        })
        setPayloads(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(
          err instanceof Error ? err.message : 'Failed to fetch payloads',
        )
      } finally {
        setLoading(false)
      }
    },
    [sessionId, page, pageSize],
  )

  useEffect(() => {
    void load()
  }, [load])

  const handleViewPayload = useCallback(async (payloadId: number) => {
    setPayloadLoading(true)
    try {
      const p = await fetchPayload(payloadId)
      setViewPayload(p)
    } catch {
      setViewPayload(null)
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
        header: 'Content Type',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.contentType || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'bodyText',
        header: 'Preview',
        cell: ({ row }) => (
          <span className="inline-block max-w-[200px] truncate text-xs text-muted-foreground">
            {row.original.bodyText
              ? row.original.bodyText.slice(0, 80) +
                (row.original.bodyText.length > 80 ? '...' : '')
              : '-'}
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
              onClick={() => handleViewPayload(row.original.payloadId)}
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

  if (loading) return <LoadingState />
  if (error) return <ErrorBanner error={error} />

  return (
    <>
      <div className="mb-2 flex items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => load()}
        >
          <RiRefreshLine className="size-3" />
          Refresh
        </Button>
      </div>
      <DataTable
        columns={columns}
        data={payloads}
        emptyMessage="No payloads found."
      />
      <ServerPaginationControls
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        onPageChange={setPage}
        onPageSizeChange={(nextPageSize) => {
          setPageSize(nextPageSize)
          setPage(1)
        }}
        totalLabel="records"
      />

      {/* Payload detail modal */}
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
            onClick={() => copyEvidence(viewPayload?.bodyText)}
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
              onClick={() => copyEvidence(viewPayload?.parsed)}
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

// --- Dialogs Tab ---
function DialogsTab({ sessionId }: { sessionId: string }) {
  const [dialogs, setDialogs] = useState<Array<SessionDialog>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(
    async (p?: number) => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetchSessionDialogs(sessionId, {
          page: p ?? page,
          pageSize,
        })
        setDialogs(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch dialogs')
      } finally {
        setLoading(false)
      }
    },
    [sessionId, page, pageSize],
  )

  useEffect(() => {
    void load()
  }, [load])

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
          <span className="inline-block max-w-[120px] truncate font-mono text-[10px] text-muted-foreground">
            {row.original.sipCallId || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'fromTag',
        header: 'From Tag',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.fromTag || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'toTag',
        header: 'To Tag',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.toTag || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'remoteContact',
        header: 'Remote Contact',
        cell: ({ row }) => (
          <span className="inline-block max-w-[160px] truncate text-xs text-muted-foreground">
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
        header: 'Route Set',
        cell: ({ row }) => (
          <span className="inline-block max-w-[160px] truncate text-[10px] text-muted-foreground">
            {row.original.routeSet?.length
              ? row.original.routeSet.join(', ')
              : '-'}
          </span>
        ),
      },
    ],
    [],
  )

  if (loading) return <LoadingState />
  if (error) return <ErrorBanner error={error} />

  return (
    <>
      <div className="mb-2 flex items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => load()}
        >
          <RiRefreshLine className="size-3" />
          Refresh
        </Button>
      </div>
      <DataTable
        columns={columns}
        data={dialogs}
        emptyMessage="No dialogs found."
      />
      <ServerPaginationControls
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        onPageChange={setPage}
        onPageSizeChange={(nextPageSize) => {
          setPageSize(nextPageSize)
          setPage(1)
        }}
        totalLabel="records"
      />
    </>
  )
}

// --- Stats Tab ---
function StatsTab({ sessionId }: { sessionId: string }) {
  const [stats, setStats] = useState<Array<SessionStats>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [viewData, setViewData] = useState<Record<string, unknown> | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(
    async (p?: number) => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetchSessionStats(sessionId, {
          page: p ?? page,
          pageSize,
        })
        setStats(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch stats')
      } finally {
        setLoading(false)
      }
    },
    [sessionId, page, pageSize],
  )

  useEffect(() => {
    void load()
  }, [load])

  const columns = useMemo<Array<ColumnDef<SessionStats>>>(
    () => [
      {
        id: 'details',
        header: 'Details',
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
        accessorKey: 'pliSent',
        header: 'PLI Sent',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.pliSent}</span>
        ),
      },
      {
        accessorKey: 'pliResponse',
        header: 'PLI Response',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.pliResponse}</span>
        ),
      },
      {
        accessorKey: 'lastPliSentAt',
        header: 'Last PLI Sent',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.lastPliSentAt
              ? formatTimestamp(row.original.lastPliSentAt)
              : '-'}
          </span>
        ),
      },
      {
        accessorKey: 'lastKeyframeAt',
        header: 'Last Keyframe',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.lastKeyframeAt
              ? formatTimestamp(row.original.lastKeyframeAt)
              : '-'}
          </span>
        ),
      },
      {
        accessorKey: 'audioRtcpRr',
        header: 'Audio RR',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.audioRtcpRr}</span>
        ),
      },
      {
        accessorKey: 'audioRtcpSr',
        header: 'Audio SR',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.audioRtcpSr}</span>
        ),
      },
      {
        accessorKey: 'videoRtcpRr',
        header: 'Video RR',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.videoRtcpRr}</span>
        ),
      },
      {
        accessorKey: 'videoRtcpSr',
        header: 'Video SR',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.videoRtcpSr}</span>
        ),
      },
    ],
    [],
  )

  if (loading) return <LoadingState />
  if (error) return <ErrorBanner error={error} />

  return (
    <>
      <div className="mb-2 flex items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => load()}
        >
          <RiRefreshLine className="size-3" />
          Refresh
        </Button>
      </div>
      <DataTable
        columns={columns}
        data={stats}
        emptyMessage="No stats found."
      />
      <ServerPaginationControls
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        onPageChange={setPage}
        onPageSizeChange={(nextPageSize) => {
          setPageSize(nextPageSize)
          setPage(1)
        }}
        totalLabel="records"
      />
      <Dialog
        open={!!viewData}
        onOpenChange={(open) => {
          if (!open) setViewData(null)
        }}
      >
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Statistics details</DialogTitle>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {formatEvidence(viewData)}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  )
}
