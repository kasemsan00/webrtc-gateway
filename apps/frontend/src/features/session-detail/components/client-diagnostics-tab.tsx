import { RiLoader4Line, RiRefreshLine } from '@remixicon/react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type {
  SessionEvent,
  SessionPayload,
} from '@/features/session-detail/types'

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
import { formatThaiDateTime } from '@/lib/date-time'
import {
  fetchClientDiagnosticPayload,
  fetchClientDiagnosticSessionEvents,
  fetchClientDiagnosticSessionPayloads,
} from '@/features/client-diagnostics/services/client-diagnostics-api'

const DEFAULT_PAGE_SIZE = 50

const formatTimestamp = (iso: string) =>
  formatThaiDateTime(iso, { fractionalSecondDigits: 3 })

type SubTab = 'events' | 'payloads'

export function ClientDiagnosticsTab({ sessionId }: { sessionId: string }) {
  const [subTab, setSubTab] = useState<SubTab>('events')

  return (
    <div>
      <div className="mb-3 flex gap-1">
        {(['events', 'payloads'] as const).map((t) => (
          <Button
            key={t}
            size="sm"
            variant={subTab === t ? 'secondary' : 'outline'}
            className="h-7 px-2 text-xs capitalize"
            onClick={() => setSubTab(t)}
          >
            {t}
          </Button>
        ))}
      </div>
      {subTab === 'events' ? (
        <ClientDiagEventsPanel sessionId={sessionId} />
      ) : (
        <ClientDiagPayloadsPanel sessionId={sessionId} />
      )}
    </div>
  )
}

function ClientDiagEventsPanel({ sessionId }: { sessionId: string }) {
  const [events, setEvents] = useState<Array<SessionEvent>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetchClientDiagnosticSessionEvents(sessionId, {
        page,
        pageSize,
      })
      setEvents(res.items)
      setTotal(res.total)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Failed to fetch client events',
      )
    } finally {
      setLoading(false)
    }
  }, [sessionId, page, pageSize])

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
      { accessorKey: 'name', header: 'Name' },
      { accessorKey: 'category', header: 'Category' },
    ],
    [],
  )

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
      </div>
    )
  }
  if (error) {
    return (
      <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
        {error}
      </div>
    )
  }

  return (
    <>
      <div className="mb-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => void load()}
        >
          <RiRefreshLine className="size-3" />
          Refresh
        </Button>
      </div>
      <DataTable
        columns={columns}
        data={events}
        emptyMessage="No mobile client diagnostics for this session."
      />
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
        totalLabel="events"
      />
    </>
  )
}

function ClientDiagPayloadsPanel({ sessionId }: { sessionId: string }) {
  const [payloads, setPayloads] = useState<Array<SessionPayload>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [viewPayload, setViewPayload] = useState<SessionPayload | null>(null)
  const [payloadLoading, setPayloadLoading] = useState(false)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetchClientDiagnosticSessionPayloads(sessionId, {
        page,
        pageSize,
      })
      setPayloads(res.items)
      setTotal(res.total)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Failed to fetch client payloads',
      )
    } finally {
      setLoading(false)
    }
  }, [sessionId, page, pageSize])

  useEffect(() => {
    void load()
  }, [load])

  const handleView = useCallback(async (payloadId: number) => {
    setPayloadLoading(true)
    try {
      const p = await fetchClientDiagnosticPayload(payloadId)
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
        accessorKey: 'kind',
        header: 'Kind',
        cell: ({ row }) => (
          <Badge variant="outline" className="text-[10px]">
            {row.original.kind}
          </Badge>
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
              disabled={payloadLoading}
              onClick={() => void handleView(row.original.payloadId)}
            >
              View
            </Button>
          </div>
        ),
      },
    ],
    [handleView, payloadLoading],
  )

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
      </div>
    )
  }
  if (error) {
    return (
      <div className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
        {error}
      </div>
    )
  }

  return (
    <>
      <DataTable
        columns={columns}
        data={payloads}
        emptyMessage="No mobile client diagnostics for this session."
      />
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
              Client diagnostics #{viewPayload?.payloadId}
            </DialogTitle>
            <DialogDescription>{viewPayload?.kind}</DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs">
            {viewPayload?.bodyText || '(empty)'}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  )
}
