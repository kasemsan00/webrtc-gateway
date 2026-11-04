import { RiComputerLine, RiLoader4Line, RiRefreshLine } from '@remixicon/react'
import { Link, useSearch } from '@tanstack/react-router'
import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { GatewayInstance } from '@/features/gateway-instances/types'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { IdCell, TimestampCell } from '@/components/ui/table-cells'
import { fetchGatewayInstances } from '@/features/gateway-instances/services/gateway-instances-api'
import {
  refreshGatewayStatus,
  useGatewayStatus,
} from '@/features/gateway-status/gateway-status-store'
import {
  OVERALL_LABEL,
  OVERALL_TONE,
  TONE_CLASSES,
  overallHealth,
} from '@/features/gateway-status/health'
import { formatRelativeTime } from '@/lib/date-time'
import { useNow } from '@/lib/use-now'
import { useServerListController } from '@/lib/use-server-list-controller'
import { cn } from '@/lib/utils'

function formatUptime(seconds: number) {
  const total = Math.max(0, Math.floor(seconds))
  const days = Math.floor(total / 86_400)
  const hours = Math.floor((total % 86_400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  return days > 0 ? `${days}d ${hours}h ${minutes}m` : `${hours}h ${minutes}m`
}

function Stat({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="min-w-0">
      <p className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      <div className="truncate text-sm font-medium">{children}</div>
    </div>
  )
}

export function GatewayInstancesPage() {
  const routeSearch = useSearch({ from: '/instances' })
  const status = useGatewayStatus()
  const now = useNow(5000)
  const {
    items: instances,
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
  } = useServerListController(fetchGatewayInstances, {
    initialSearch: routeSearch.search ?? '',
  })

  const thisInstance = status.dashboard?.instanceId
  const overall = overallHealth(status.health?.components)
  const tone = TONE_CLASSES[OVERALL_TONE[overall]]
  const aliveCount = instances.filter((i) => !i.isExpired).length

  const columns = useMemo<Array<ColumnDef<GatewayInstance>>>(
    () => [
      {
        accessorKey: 'instanceId',
        header: 'Instance',
        cell: ({ row }) => (
          <span className="inline-flex items-center gap-1.5">
            <IdCell
              value={row.original.instanceId}
              label="Instance ID"
              maxWidth="max-w-[220px]"
            />
            {row.original.instanceId === thisInstance ? (
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
                ? 'No heartbeat before the lease expired: the instance is down or cannot reach the shared store.'
                : 'Heartbeat is current.'
            }
          >
            {row.original.isExpired ? 'Stopped reporting' : 'Alive'}
          </Badge>
        ),
      },
      {
        accessorKey: 'updatedAt',
        header: 'Last heartbeat',
        cell: ({ row }) => (
          <TimestampCell value={row.original.updatedAt} relative now={now} />
        ),
      },
      {
        accessorKey: 'expiresAt',
        header: 'Lease expires',
        cell: ({ row }) => (
          <TimestampCell value={row.original.expiresAt} relative now={now} />
        ),
      },
      {
        accessorKey: 'wsUrl',
        header: 'WebSocket URL',
        cell: ({ row }) => (
          <IdCell
            value={row.original.wsUrl}
            label="WebSocket URL"
            maxWidth="max-w-[280px]"
          />
        ),
      },
      {
        id: 'links',
        header: () => <div className="text-right">Owned</div>,
        cell: ({ row }) => (
          <div className="flex justify-end gap-2 text-xs">
            <Link
              to="/session-directory"
              search={{ search: row.original.instanceId }}
              className="text-cyan-700 hover:underline dark:text-cyan-400"
            >
              sessions
            </Link>
          </div>
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
          placeholder="Search instance or URL…"
          aria-label="Search instances"
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
          onClick={() => {
            reload()
            void refreshGatewayStatus()
          }}
          disabled={loading}
        >
          <RiRefreshLine
            className={cn('size-3.5', loading && 'animate-spin')}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        <section className="mb-4 rounded-lg border border-border p-4">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold">
              Instance serving this console
            </h2>
            <span
              className={cn(
                'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] font-medium',
                tone.border,
                tone.text,
              )}
            >
              <span className={cn('size-2 rounded-full', tone.dot)} />
              {OVERALL_LABEL[overall]}
            </span>
            <Link
              to="/"
              className="text-[11px] text-cyan-700 hover:underline dark:text-cyan-400"
            >
              component health →
            </Link>
          </div>
          {status.error && !status.dashboard ? (
            <p className="text-sm text-rose-600 dark:text-rose-400">
              {status.error}
            </p>
          ) : (
            <div className="grid gap-4 sm:grid-cols-3 xl:grid-cols-6">
              <Stat label="Instance ID">
                <span className="font-mono text-xs">{thisInstance ?? '…'}</span>
              </Stat>
              <Stat label="Version">
                <span className="font-mono text-xs">
                  {status.build?.version ?? '-'}
                  {status.build?.commit
                    ? ` · ${status.build.commit.slice(0, 7)}`
                    : ''}
                </span>
              </Stat>
              <Stat label="Uptime">
                {status.dashboard
                  ? formatUptime(status.dashboard.uptimeSeconds)
                  : '-'}
              </Stat>
              <Stat label="Live calls">
                <Link
                  to="/active-sessions"
                  search={{}}
                  className="hover:underline"
                >
                  {status.dashboard?.activeSessions ?? '-'}
                </Link>
              </Stat>
              <Stat label="WS clients">
                <Link to="/ws-clients" search={{}} className="hover:underline">
                  {status.dashboard?.wsClients ?? '-'}
                </Link>
              </Stat>
              <Stat label="Trunks registered">
                <Link to="/trunks" search={{}} className="hover:underline">
                  {status.dashboard
                    ? `${status.dashboard.registeredTrunks} / ${status.dashboard.enabledTrunks}`
                    : '-'}
                </Link>
              </Stat>
            </div>
          )}
          {status.updatedAt ? (
            <p className="mt-3 text-[10px] text-muted-foreground">
              checked {formatRelativeTime(status.updatedAt, now)}
            </p>
          ) : null}
        </section>

        <div className="mb-2 flex flex-wrap items-baseline gap-x-3 text-xs text-muted-foreground">
          <h2 className="text-sm font-semibold text-foreground">
            All instances
          </h2>
          <span>
            {aliveCount} alive · {instances.length - aliveCount} stopped
            reporting
            {total > instances.length ? ` (this page of ${total})` : ''}
          </span>
          <span>
            Each instance renews a heartbeat lease; one that stops renewing is
            marked as stopped.
          </span>
        </div>

        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
            {error}
          </div>
        ) : null}

        {initialLoading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : instances.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiComputerLine className="size-10 opacity-30" />
            <p className="text-sm">No gateway instances found</p>
            <p className="text-xs">
              Instances register here when the shared session directory is
              enabled.
            </p>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={instances}
            getRowClassName={(i) => (i.isExpired ? 'opacity-70' : undefined)}
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
