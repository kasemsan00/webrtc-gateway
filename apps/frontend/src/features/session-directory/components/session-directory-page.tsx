import { RiLoader4Line, RiRefreshLine, RiRouteLine } from '@remixicon/react'
import { useMemo } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'

import type { SessionDirectoryEntry } from '@/features/session-directory/types'

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
import { useGatewayStatus } from '@/features/gateway-status/gateway-status-store'
import { fetchSessionDirectory } from '@/features/session-directory/services/session-directory-api'
import { instanceSearchFrom } from '@/lib/operations-navigation'
import { useNow } from '@/lib/use-now'
import { useServerListController } from '@/lib/use-server-list-controller'
import { cn } from '@/lib/utils'

export function SessionDirectoryPage() {
  const routeSearch = useSearch({ from: '/session-directory' })
  const { dashboard } = useGatewayStatus()
  const now = useNow(5000)
  const {
    items: entries,
    page,
    pageSize,
    search,
    loading,
    initialLoading,
    error,
    totalPages,
    total,
    setSearch,
    handlePageChange,
    handlePageSizeChange,
    debouncedSearch,
    reload,
  } = useServerListController(fetchSessionDirectory, {
    initialSearch: routeSearch.search ?? '',
  })

  const thisInstance = dashboard?.instanceId

  const columns = useMemo<Array<ColumnDef<SessionDirectoryEntry>>>(
    () => [
      {
        accessorKey: 'sessionId',
        header: 'Session',
        cell: ({ row }) => <SessionIdLink sessionId={row.original.sessionId} />,
      },
      {
        accessorKey: 'ownerInstanceId',
        header: 'Owner instance',
        cell: ({ row }) => (
          <span className="inline-flex items-center gap-1.5">
            <Link
              to="/instances"
              search={instanceSearchFrom(row.original.ownerInstanceId)}
              className="inline-block max-w-[200px] truncate font-mono text-xs text-cyan-700 hover:underline dark:text-cyan-400"
              title={row.original.ownerInstanceId}
            >
              {row.original.ownerInstanceId}
            </Link>
            {row.original.ownerInstanceId === thisInstance ? (
              <Badge variant="default" className="text-[9px]">
                this console
              </Badge>
            ) : null}
          </span>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: ({ row }) => (
          <Badge
            variant={row.original.isExpired ? 'destructive' : 'success'}
            className="text-[10px]"
            title={
              row.original.isExpired
                ? 'The owner stopped renewing this entry: the call likely ended or the instance died.'
                : 'Owner is renewing this entry.'
            }
          >
            {row.original.isExpired ? 'Expired' : 'Routed'}
          </Badge>
        ),
      },
      {
        accessorKey: 'updatedAt',
        header: 'Last renewed',
        cell: ({ row }) => (
          <TimestampCell value={row.original.updatedAt} relative now={now} />
        ),
      },
      {
        accessorKey: 'expiresAt',
        header: 'Expires',
        cell: ({ row }) => (
          <TimestampCell value={row.original.expiresAt} relative now={now} />
        ),
      },
      {
        accessorKey: 'wsUrl',
        header: 'Owner WebSocket URL',
        cell: ({ row }) => (
          <IdCell
            value={row.original.wsUrl}
            label="WebSocket URL"
            maxWidth="max-w-[240px]"
          />
        ),
      },
    ],
    [now, thisInstance],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <Input
          className="h-7 w-40 text-xs sm:w-56"
          placeholder="Search session, instance, URL…"
          aria-label="Search session directory"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value)
            debouncedSearch(e.target.value)
          }}
        />
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={reload}
          disabled={loading}
        >
          <RiRefreshLine
            className={cn('size-3.5', loading && 'animate-spin')}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        <p className="mb-3 max-w-3xl text-xs text-muted-foreground">
          When several gateway instances run behind one load balancer, a client
          may reconnect to a different instance. This directory records which
          instance owns each live call so resume, hangup and DTMF requests are
          forwarded to the right one. An expired entry means the owner stopped
          renewing it.
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
        ) : entries.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiRouteLine className="size-10 opacity-30" />
            <p className="text-sm">No session directory entries</p>
            <p className="text-xs">
              Entries exist only while calls are live on a multi-instance setup.
            </p>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={entries}
            getRowClassName={(e) => (e.isExpired ? 'opacity-70' : undefined)}
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
