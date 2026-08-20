import {
  RiBugLine,
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'

import type { ClientDiagnostic } from '@/features/client-diagnostics/types'
import type { SessionPayload } from '@/features/session-detail/types'

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
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { formatThaiDateTime } from '@/lib/date-time'
import { useTheme } from '@/lib/theme'
import {
  fetchClientDiagnosticPayload,
  fetchClientDiagnostics,
} from '@/features/client-diagnostics/services/client-diagnostics-api'

const DEFAULT_PAGE_SIZE = 50

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
    case 'verbose':
    case 'debug':
      return 'secondary'
    default:
      return 'secondary'
  }
}

function formatTimestamp(iso: string) {
  return formatThaiDateTime(iso, { fractionalSecondDigits: 3 })
}

function getDiagnosticPayloadId(
  data: Record<string, unknown> | undefined,
): number | null {
  if (!data) return null

  const raw = data.payloadId
  if (typeof raw === 'number' && Number.isFinite(raw)) {
    return raw
  }

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

export function ClientDiagnosticsPage() {
  const { theme, toggleTheme } = useTheme()
  const [items, setItems] = useState<Array<ClientDiagnostic>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dbUnavailable, setDbUnavailable] = useState(false)

  const [clientTraceId, setClientTraceId] = useState('')
  const [authSubject, setAuthSubject] = useState('')
  const [source, setSource] = useState('')
  const [level, setLevel] = useState('')
  const [name, setName] = useState('')
  const [viewPayload, setViewPayload] = useState<SessionPayload | null>(null)
  const [payloadLoading, setPayloadLoading] = useState(false)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const handleViewPayload = useCallback(async (payloadId: number) => {
    setPayloadLoading(true)
    try {
      const payload = await fetchClientDiagnosticPayload(payloadId)
      setViewPayload(payload)
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : 'Failed to fetch diagnostic payload',
      )
    } finally {
      setPayloadLoading(false)
    }
  }, [])

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    setDbUnavailable(false)
    try {
      const response = await fetchClientDiagnostics({
        page,
        pageSize,
        clientTraceId: clientTraceId.trim() || undefined,
        authSubject: authSubject.trim() || undefined,
        source: source.trim() || undefined,
        level: level.trim() || undefined,
        name: name.trim() || undefined,
      })
      setItems(response.items)
      setTotal(response.total)
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to fetch diagnostics'
      if (
        message.includes('503') ||
        message.toLowerCase().includes('not available')
      ) {
        setDbUnavailable(true)
      }
      setError(message)
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, clientTraceId, authSubject, source, level, name])

  useEffect(() => {
    void load()
  }, [load])

  const columns = useMemo<Array<ColumnDef<ClientDiagnostic>>>(
    () => [
      {
        accessorKey: 'id',
        header: 'ID',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            #{row.original.id}
          </span>
        ),
      },
      {
        accessorKey: 'timestamp',
        header: 'Time',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {formatTimestamp(row.original.timestamp)}
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
        header: 'Name',
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.name}</span>
        ),
      },
      {
        accessorKey: 'source',
        header: 'Source',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.source}</span>
        ),
      },
      {
        accessorKey: 'platform',
        header: 'Platform',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.platform || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'preferredUsername',
        header: 'User',
        cell: ({ row }) => (
          <span className="text-xs">
            {row.original.preferredUsername || row.original.authSubject || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'clientTraceId',
        header: 'Trace ID',
        cell: ({ row }) => (
          <span className="font-mono text-[10px] text-muted-foreground">
            {row.original.clientTraceId || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'appVersion',
        header: 'App',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.appVersion || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'authRealm',
        header: 'Realm',
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.authRealm || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'deviceIdHash',
        header: 'Device',
        cell: ({ row }) => (
          <span className="inline-block max-w-[100px] truncate font-mono text-[10px] text-muted-foreground">
            {row.original.deviceIdHash || '-'}
          </span>
        ),
      },
      {
        id: 'session',
        header: 'Session',
        cell: ({ row }) => {
          const sessionId = getDiagnosticSessionId(row.original.data)
          return sessionId ? (
            <Link
              to="/sessions/$sessionId"
              params={{ sessionId }}
              className="font-mono text-[10px] text-cyan-600 hover:underline dark:text-cyan-400"
            >
              {sessionId}
            </Link>
          ) : (
            <span className="text-xs text-muted-foreground">-</span>
          )
        },
      },
      {
        accessorKey: 'data',
        header: 'Data',
        cell: ({ row }) => {
          const data = row.original.data
          if (!data || Object.keys(data).length === 0) {
            return <span className="text-xs text-muted-foreground">-</span>
          }
          const preview = JSON.stringify(data)
          return (
            <span className="text-xs text-muted-foreground" title={preview}>
              {preview.length > 80 ? `${preview.slice(0, 80)}...` : preview}
            </span>
          )
        },
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        cell: ({ row }) => {
          const payloadId = getDiagnosticPayloadId(row.original.data)
          if (!payloadId) {
            return <span className="text-xs text-muted-foreground">-</span>
          }

          return (
            <div className="flex justify-end">
              <Button
                size="sm"
                variant="secondary"
                className="h-6 px-2 text-[10px]"
                onClick={() => {
                  void handleViewPayload(payloadId)
                }}
                disabled={payloadLoading}
              >
                View
              </Button>
            </div>
          )
        },
      },
    ],
    [handleViewPayload, payloadLoading],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => load()}
            disabled={loading}
          >
            <RiRefreshLine
              className={`size-3.5 ${loading ? 'animate-spin' : ''}`}
            />
            Refresh
          </Button>
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

      <div className="flex-1 overflow-y-auto p-4">
        {dbUnavailable ? (
          <div className="mb-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-500">
            Database logging not available. Client diagnostics require
            DB_ENABLE.
          </div>
        ) : null}

        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}

        <div className="mb-3 grid gap-2 md:grid-cols-2 xl:grid-cols-5">
          <Input
            value={clientTraceId}
            onChange={(event) => {
              setClientTraceId(event.target.value)
              setPage(1)
            }}
            placeholder="clientTraceId"
            className="h-8 text-xs"
          />
          <Input
            value={authSubject}
            onChange={(event) => {
              setAuthSubject(event.target.value)
              setPage(1)
            }}
            placeholder="authSubject"
            className="h-8 text-xs"
          />
          <Input
            value={source}
            onChange={(event) => {
              setSource(event.target.value)
              setPage(1)
            }}
            placeholder="source"
            className="h-8 text-xs"
          />
          <Input
            value={level}
            onChange={(event) => {
              setLevel(event.target.value)
              setPage(1)
            }}
            placeholder="level (error, info, ...)"
            className="h-8 text-xs"
          />
          <Input
            value={name}
            onChange={(event) => {
              setName(event.target.value)
              setPage(1)
            }}
            placeholder="name"
            className="h-8 text-xs"
          />
        </div>

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiBugLine className="size-10 opacity-30" />
            <p className="text-sm">No client diagnostics found</p>
          </div>
        ) : (
          <>
            <DataTable columns={columns} data={items} />
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
            />
          </>
        )}
      </div>

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
            {viewPayload?.bodyText || '(empty)'}
          </pre>
        </DialogContent>
      </Dialog>
    </div>
  )
}
