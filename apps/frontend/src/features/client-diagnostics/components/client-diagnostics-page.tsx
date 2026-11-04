import {
  RiBugLine,
  RiCloseLine,
  RiFilter3Line,
  RiLoader4Line,
  RiRefreshLine,
} from '@remixicon/react'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'

import type { ClientDiagnostic } from '@/features/client-diagnostics/types'
import type { SessionPayload } from '@/features/session-detail/types'
import type { ClientDiagnosticsSearch } from '@/lib/search-params'

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
import { Input } from '@/components/ui/input'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { IdCell, SessionIdLink, copyText } from '@/components/ui/table-cells'
import {
  fetchClientDiagnosticPayload,
  fetchClientDiagnostics,
} from '@/features/client-diagnostics/services/client-diagnostics-api'
import { HttpError } from '@/lib/http-client'
import { formatPreciseDateTime, formatRelativeTime } from '@/lib/date-time'
import { useNow } from '@/lib/use-now'
import { usePagedList } from '@/lib/use-paged-list'
import { cn } from '@/lib/utils'

const DEFAULT_PAGE_SIZE = 50

const LEVEL_OPTIONS = ['error', 'warn', 'info', 'debug', 'verbose'] as const

const TEXT_FILTERS: Array<{
  key: keyof Omit<ClientDiagnosticsSearch, 'level'>
  label: string
  placeholder: string
  hint: string
}> = [
  {
    key: 'authSubject',
    label: 'User (auth subject)',
    placeholder: 'Keycloak subject ID',
    hint: 'The signed-in user (JWT `sub`). Copy it from WS Clients.',
  },
  {
    key: 'clientTraceId',
    label: 'Client trace ID',
    placeholder: 'trace ID from the app',
    hint: 'Correlates every event from one app run / call attempt.',
  },
  {
    key: 'name',
    label: 'Event name',
    placeholder: 'e.g. ws_reconnect',
    hint: 'Exact event name reported by the client.',
  },
  {
    key: 'source',
    label: 'Source',
    placeholder: 'e.g. android, ios, electron',
    hint: 'Which client component produced the event.',
  },
]

function levelVariant(
  level: string,
): 'default' | 'secondary' | 'destructive' | 'success' | 'warning' {
  switch (level.toLowerCase()) {
    case 'error':
      return 'destructive'
    case 'warn':
    case 'warning':
      return 'warning'
    case 'info':
      return 'default'
    default:
      return 'secondary'
  }
}

function formatTimestamp(iso: string) {
  return formatPreciseDateTime(iso)
}

function getDiagnosticPayloadId(
  data: Record<string, unknown> | undefined,
): number | null {
  if (!data) return null
  const raw = data.payloadId
  if (typeof raw === 'number' && Number.isFinite(raw)) return raw
  if (typeof raw === 'string' && raw.trim() !== '') {
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

function getDiagnosticSessionId(data: Record<string, unknown> | undefined) {
  const raw = data?.sessionId
  return typeof raw === 'string' && raw.trim() !== '' ? raw : null
}

function FilterChip({
  onClick,
  title,
}: {
  onClick: () => void
  title: string
}) {
  return (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation()
        onClick()
      }}
      className="inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-60 hover:bg-muted hover:text-foreground hover:opacity-100"
      title={title}
      aria-label={title}
    >
      <RiFilter3Line className="size-3" />
    </button>
  )
}

export function ClientDiagnosticsPage() {
  const search = useSearch({ from: '/client-diagnostics' })
  const navigate = useNavigate({ from: '/client-diagnostics' })
  const [drafts, setDrafts] = useState<ClientDiagnosticsSearch>(search)
  const [viewPayload, setViewPayload] = useState<SessionPayload | null>(null)
  const [viewData, setViewData] = useState<ClientDiagnostic | null>(null)
  const [payloadError, setPayloadError] = useState<string | null>(null)
  const now = useNow(10_000)

  useEffect(() => {
    setDrafts(search)
  }, [search])

  const list = usePagedList<ClientDiagnostic>(
    (page, pageSize) =>
      fetchClientDiagnostics({
        page,
        pageSize,
        clientTraceId: search.clientTraceId,
        authSubject: search.authSubject,
        source: search.source,
        level: search.level,
        name: search.name,
      }),
    [search],
    DEFAULT_PAGE_SIZE,
  )

  const dbUnavailable =
    !!list.error &&
    (list.error.includes('HTTP 503') ||
      list.error.toLowerCase().includes('not available'))

  const setFilter = (patch: Partial<ClientDiagnosticsSearch>) => {
    void navigate({
      search: (prev: ClientDiagnosticsSearch) => {
        const next: ClientDiagnosticsSearch = { ...prev, ...patch }
        for (const key of Object.keys(next) as Array<
          keyof ClientDiagnosticsSearch
        >) {
          if (!next[key]) delete next[key]
        }
        return next
      },
      replace: true,
    })
  }

  const activeFilters = Object.values(search).filter(Boolean).length

  const handleViewPayload = async (payloadId: number) => {
    setPayloadError(null)
    try {
      setViewPayload(await fetchClientDiagnosticPayload(payloadId))
    } catch (err) {
      setPayloadError(
        err instanceof HttpError || err instanceof Error
          ? err.message
          : 'Failed to fetch diagnostic payload',
      )
    }
  }

  const columns = useMemo<Array<ColumnDef<ClientDiagnostic>>>(
    () => [
      {
        accessorKey: 'timestamp',
        header: 'Time',
        cell: ({ row }) => (
          <span
            className="whitespace-nowrap text-xs text-muted-foreground"
            title={formatTimestamp(row.original.timestamp)}
          >
            {formatTimestamp(row.original.timestamp)}
            <span className="block text-[10px]">
              {formatRelativeTime(row.original.timestamp, now)}
            </span>
          </span>
        ),
      },
      {
        accessorKey: 'level',
        header: 'Level',
        cell: ({ row }) => (
          <Badge
            variant={levelVariant(row.original.level)}
            className="text-[10px]"
          >
            {row.original.level}
          </Badge>
        ),
      },
      {
        accessorKey: 'name',
        header: 'Event',
        cell: ({ row }) => (
          <div className="min-w-0 text-xs">
            <span className="inline-flex items-center gap-0.5">
              <span className="font-mono">{row.original.name}</span>
              <FilterChip
                title={`Show only "${row.original.name}"`}
                onClick={() => setFilter({ name: row.original.name })}
              />
            </span>
            <p className="text-[10px] text-muted-foreground">
              {row.original.source}
            </p>
          </div>
        ),
      },
      {
        id: 'user',
        header: 'User',
        cell: ({ row }) => {
          const d = row.original
          return (
            <div className="text-xs">
              <span className="inline-flex items-center gap-0.5">
                <span>{d.preferredUsername || '-'}</span>
                {d.authSubject ? (
                  <FilterChip
                    title="Show only this user"
                    onClick={() => setFilter({ authSubject: d.authSubject })}
                  />
                ) : null}
              </span>
              <p className="text-[10px] text-muted-foreground">
                {d.authRealm || ''}
              </p>
            </div>
          )
        },
      },
      {
        id: 'device',
        header: 'Device / app',
        cell: ({ row }) => {
          const d = row.original
          return (
            <div className="text-xs">
              <p>
                {d.platform || '-'}
                {d.appVersion ? (
                  <span className="text-muted-foreground">
                    {' '}
                    · v{d.appVersion}
                  </span>
                ) : null}
              </p>
              {d.deviceIdHash ? (
                <IdCell
                  value={d.deviceIdHash}
                  label="Device hash"
                  maxWidth="max-w-[80px]"
                />
              ) : null}
            </div>
          )
        },
      },
      {
        id: 'session',
        header: 'Session',
        cell: ({ row }) => (
          <SessionIdLink
            sessionId={getDiagnosticSessionId(row.original.data)}
            maxWidth="max-w-[110px]"
          />
        ),
      },
      {
        accessorKey: 'clientTraceId',
        header: 'Trace',
        cell: ({ row }) =>
          row.original.clientTraceId ? (
            <span className="inline-flex items-center gap-0.5">
              <IdCell
                value={row.original.clientTraceId}
                label="Trace ID"
                maxWidth="max-w-[90px]"
              />
              <FilterChip
                title="Show the whole trace"
                onClick={() =>
                  setFilter({ clientTraceId: row.original.clientTraceId })
                }
              />
            </span>
          ) : (
            <span className="text-xs text-muted-foreground">-</span>
          ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Data</div>,
        cell: ({ row }) => {
          const payloadId = getDiagnosticPayloadId(row.original.data)
          const hasData =
            row.original.data && Object.keys(row.original.data).length > 0
          return (
            <div className="flex justify-end gap-1">
              {hasData ? (
                <Button
                  size="sm"
                  variant="outline"
                  className="h-6 px-2 text-[10px]"
                  onClick={() => setViewData(row.original)}
                  title={JSON.stringify(row.original.data).slice(0, 300)}
                >
                  JSON
                </Button>
              ) : null}
              {payloadId ? (
                <Button
                  size="sm"
                  variant="secondary"
                  className="h-6 px-2 text-[10px]"
                  onClick={() => void handleViewPayload(payloadId)}
                >
                  Payload
                </Button>
              ) : null}
              {!hasData && !payloadId ? (
                <span className="text-xs text-muted-foreground">-</span>
              ) : null}
            </div>
          )
        },
      },
    ],
    // setFilter only uses navigate, which is stable.
    [now],
  )

  const filters = (
    <div className="flex flex-wrap items-end gap-2 text-xs">
      {TEXT_FILTERS.map((filter) => (
        <label
          key={filter.key}
          className="flex flex-col gap-0.5"
          title={filter.hint}
        >
          <span className="text-[10px] text-muted-foreground">
            {filter.label}
          </span>
          <Input
            value={drafts[filter.key] ?? ''}
            onChange={(event) =>
              setDrafts((prev) => ({
                ...prev,
                [filter.key]: event.target.value,
              }))
            }
            onBlur={(event) =>
              setFilter({
                [filter.key]: event.target.value.trim() || undefined,
              })
            }
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                setFilter({
                  [filter.key]: event.currentTarget.value.trim() || undefined,
                })
              }
            }}
            placeholder={filter.placeholder}
            className="h-7 w-44 text-xs"
          />
        </label>
      ))}
      <label className="flex flex-col gap-0.5">
        <span className="text-[10px] text-muted-foreground">Level</span>
        <select
          className="h-7 rounded-md border border-border bg-background px-2 text-xs text-foreground"
          value={search.level ?? ''}
          onChange={(event) =>
            setFilter({ level: event.target.value || undefined })
          }
        >
          <option value="">Any level</option>
          {LEVEL_OPTIONS.map((level) => (
            <option key={level} value={level}>
              {level}
            </option>
          ))}
        </select>
      </label>
      {activeFilters ? (
        <Button
          size="sm"
          variant="ghost"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => void navigate({ search: {}, replace: true })}
        >
          <RiCloseLine className="size-3.5" />
          Clear filters
        </Button>
      ) : null}
      <span className="pb-1 text-[10px] text-muted-foreground">
        Press Enter to apply a text filter
      </span>
    </div>
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header subBar={filters} subBarAlways>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => void list.reload()}
          disabled={list.refreshing}
        >
          <RiRefreshLine
            className={cn('size-3.5', list.refreshing && 'animate-spin')}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        <p className="mb-3 text-xs text-muted-foreground">
          <strong className="text-foreground tabular-nums">{list.total}</strong>{' '}
          event{list.total === 1 ? '' : 's'}
          {activeFilters ? ' match the filters' : ''} · newest first · use the{' '}
          <RiFilter3Line className="inline size-3" /> icon in a row to filter by
          that value
        </p>
        {dbUnavailable ? (
          <div className="mb-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
            Database logging is not available, so client diagnostics cannot be
            stored or queried. Enable DB logging (DB_ENABLE) on the gateway.
          </div>
        ) : list.error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
            {list.error}
          </div>
        ) : null}
        {payloadError ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
            {payloadError}
          </div>
        ) : null}

        {list.loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : list.items.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiBugLine className="size-10 opacity-30" />
            <p className="text-sm">No client diagnostics found</p>
            <p className="text-xs">
              Apps upload diagnostics to POST /api/client-diagnostics.
            </p>
          </div>
        ) : (
          <>
            <DataTable
              columns={columns}
              data={list.items}
              getRowClassName={(d) =>
                d.level.toLowerCase() === 'error' ? 'bg-rose-500/5' : undefined
              }
            />
            <ServerPaginationControls
              page={list.page}
              totalPages={list.totalPages}
              total={list.total}
              pageSize={list.pageSize}
              onPageChange={list.setPage}
              onPageSizeChange={list.setPageSize}
            />
          </>
        )}
      </div>

      <Dialog
        open={!!viewData}
        onOpenChange={(open) => {
          if (!open) setViewData(null)
        }}
      >
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{viewData?.name}</DialogTitle>
            <DialogDescription>
              #{viewData?.id} ·{' '}
              {viewData ? formatTimestamp(viewData.timestamp) : ''}
            </DialogDescription>
          </DialogHeader>
          <Button
            size="sm"
            variant="outline"
            className="w-fit"
            onClick={() =>
              void copyText(
                JSON.stringify(viewData, null, 2),
                'Diagnostic JSON copied',
              )
            }
          >
            Copy full record
          </Button>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {JSON.stringify(viewData?.data ?? {}, null, 2)}
          </pre>
        </DialogContent>
      </Dialog>

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
          <Button
            size="sm"
            variant="outline"
            className="w-fit"
            onClick={() =>
              void copyText(viewPayload?.bodyText ?? '', 'Payload copied')
            }
          >
            Copy
          </Button>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {viewPayload?.bodyText || '(empty)'}
          </pre>
        </DialogContent>
      </Dialog>
    </div>
  )
}
