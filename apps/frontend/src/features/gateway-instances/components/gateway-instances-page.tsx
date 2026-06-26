import {
  RiComputerLine,
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSignalWifiLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type {
  GatewayDashboard,
  GatewayInstance,
  WSClient,
} from '@/features/gateway-instances/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
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
import { useTheme } from '@/lib/theme'
import { ExpiryStatusBadge, TimestampCell } from '@/components/ui/table-cells'
import {
  fetchGatewayDashboard,
  fetchGatewayInstances,
  fetchWSClients,
} from '@/features/gateway-instances/services/gateway-instances-api'
import { useServerListController } from '@/lib/use-server-list-controller'

function formatUptime(seconds: number) {
  const total = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const remain = total % 60
  return `${hours}h ${minutes}m ${remain}s`
}

const WS_CLIENT_PREVIEW_LIMIT = 8

function WSClientCard({ client }: { client: WSClient }) {
  return (
    <div className="rounded-md border border-border/60 px-3 py-2">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="truncate font-mono text-[11px] text-muted-foreground">
          {client.sessionId || '-'}
        </span>
        <ClientAvailabilityBadge availability={client.availability} />
      </div>
      <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[11px]">
        <span className="text-muted-foreground">Call State</span>
        <span className="text-right">{client.callState || '-'}</span>
        <span className="text-muted-foreground">Trunk</span>
        <span className="truncate text-right font-mono">
          {client.resolvedTrunkPublicId || client.resolvedTrunkId || '-'}
        </span>
        <span className="text-muted-foreground">Auth</span>
        <span className="truncate text-right">{client.authSubject || '-'}</span>
        <span className="text-muted-foreground">Connected</span>
        <span className="text-right">
          <TimestampCell value={client.connectedAt} />
        </span>
      </div>
    </div>
  )
}

function ClientAvailabilityBadge({ availability }: { availability?: string }) {
  switch (availability) {
    case 'idle':
      return (
        <Badge variant="success" className="text-[10px]">
          Idle
        </Badge>
      )
    case 'busy':
      return (
        <Badge
          variant="default"
          className="bg-amber-600/20 text-[10px] text-amber-400"
        >
          Busy
        </Badge>
      )
    case 'unavailable':
      return (
        <Badge variant="destructive" className="text-[10px]">
          Unavailable
        </Badge>
      )
    default:
      return (
        <Badge variant="outline" className="text-[10px]">
          {availability || 'Unknown'}
        </Badge>
      )
  }
}

export function GatewayInstancesPage() {
  const { theme, toggleTheme } = useTheme()
  const [dashboard, setDashboard] = useState<GatewayDashboard | null>(null)
  const [wsClients, setWsClients] = useState<Array<WSClient>>([])
  const [wsClientsDialogOpen, setWsClientsDialogOpen] = useState(false)
  const [overviewError, setOverviewError] = useState<string | null>(null)
  const {
    items: instances,
    page,
    pageSize,
    search,
    loading,
    error,
    totalPages,
    total,
    setSearch,
    handlePageChange,
    handlePageSizeChange,
    debouncedSearch,
    reload,
  } = useServerListController(fetchGatewayInstances)

  const loadOverview = useCallback(async () => {
    try {
      const [nextDashboard, nextClients] = await Promise.all([
        fetchGatewayDashboard(),
        fetchWSClients(),
      ])
      setDashboard(nextDashboard)
      setWsClients(nextClients)
      setOverviewError(null)
    } catch (err) {
      setOverviewError(
        err instanceof Error ? err.message : 'Failed to fetch gateway overview',
      )
    }
  }, [])

  useEffect(() => {
    void loadOverview()
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void loadOverview()
      }
    }, 30000)
    return () => clearInterval(timer)
  }, [loadOverview])

  const columns = useMemo<Array<ColumnDef<GatewayInstance>>>(
    () => [
      {
        accessorKey: 'instanceId',
        header: 'Instance ID',
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.instanceId}</span>
        ),
      },
      {
        accessorKey: 'wsUrl',
        header: 'WebSocket URL',
        cell: ({ row }) => (
          <span className="inline-block max-w-[300px] truncate text-xs text-muted-foreground">
            {row.original.wsUrl}
          </span>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: ({ row }) => (
          <ExpiryStatusBadge isExpired={row.original.isExpired} />
        ),
      },
      {
        accessorKey: 'expiresAt',
        header: 'Expires At',
        cell: ({ row }) => <TimestampCell value={row.original.expiresAt} />,
      },
      {
        accessorKey: 'updatedAt',
        header: 'Updated At',
        cell: ({ row }) => <TimestampCell value={row.original.updatedAt} />,
      },
    ],
    [],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <Input
            className="h-7 w-48 text-xs"
            placeholder="Search instance or URL..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              debouncedSearch(e.target.value)
            }}
          />
          <Separator orientation="vertical" className="h-4" />
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => {
              reload()
              void loadOverview()
            }}
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
        {overviewError ? (
          <div className="mb-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-400">
            {overviewError}
          </div>
        ) : null}
        {dashboard ? (
          <div className="mb-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <Card className="border-border/60">
              <CardContent className="space-y-0.5 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  Gateway
                </p>
                <p className="font-mono text-xs text-muted-foreground">
                  {dashboard.instanceId || '-'}
                </p>
                <p className="text-xs">
                  Uptime: {formatUptime(dashboard.uptimeSeconds)}
                </p>
              </CardContent>
            </Card>
            <Card className="border-border/60">
              <CardContent className="space-y-0.5 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  Calls
                </p>
                <p className="text-xs">
                  Active sessions: {dashboard.activeSessions}
                </p>
                <p className="text-xs">
                  WebSocket clients: {dashboard.wsClients}
                </p>
              </CardContent>
            </Card>
            <Card className="border-border/60">
              <CardContent className="space-y-0.5 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  Trunks
                </p>
                <p className="text-xs">
                  Registered: {dashboard.registeredTrunks}
                </p>
                <p className="text-xs">
                  Enabled / Total: {dashboard.enabledTrunks} /{' '}
                  {dashboard.totalTrunks}
                </p>
              </CardContent>
            </Card>
            <Card className="border-border/60">
              <CardContent className="space-y-0.5 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  Runtime
                </p>
                <p className="text-xs">
                  Public accounts: {dashboard.publicAccounts}
                </p>
                <p className="text-xs">
                  DB:{' '}
                  {dashboard.dbConnected ? 'Connected' : 'Disabled/Unavailable'}
                </p>
              </CardContent>
            </Card>
          </div>
        ) : null}

        {wsClients.length > 0 ? (
          <Card className="mb-4 border-border/60">
            <CardContent className="p-3">
              <div className="mb-2 flex items-center justify-between gap-2">
                <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                  <RiSignalWifiLine className="size-3.5" />
                  Live Clients ({wsClients.length})
                </div>
                {wsClients.length > WS_CLIENT_PREVIEW_LIMIT ? (
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-7 px-2 text-xs"
                    onClick={() => setWsClientsDialogOpen(true)}
                  >
                    Show all ({wsClients.length})
                  </Button>
                ) : null}
              </div>
              <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                {wsClients
                  .slice(0, WS_CLIENT_PREVIEW_LIMIT)
                  .map((client) => (
                    <WSClientCard
                      key={`${client.sessionId}-${client.connectedAt}`}
                      client={client}
                    />
                  ))}
              </div>
            </CardContent>
          </Card>
        ) : (
          <Card className="mb-4 border-border/60">
            <CardContent className="p-3">
              <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <RiSignalWifiLine className="size-3.5" />
                Live Clients
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                No connected WebSocket clients.
              </p>
            </CardContent>
          </Card>
        )}

        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : instances.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiComputerLine className="size-10 opacity-30" />
            <p className="text-sm">No gateway instances found</p>
          </div>
        ) : (
          <DataTable columns={columns} data={instances} />
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

      <Dialog open={wsClientsDialogOpen} onOpenChange={setWsClientsDialogOpen}>
        <DialogContent className="max-h-[85vh] max-w-4xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Live WebSocket Clients</DialogTitle>
            <DialogDescription>
              {wsClients.length} connected client
              {wsClients.length === 1 ? '' : 's'}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2 md:grid-cols-2">
            {wsClients.map((client) => (
              <WSClientCard
                key={`dialog-${client.sessionId}-${client.connectedAt}`}
                client={client}
              />
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
