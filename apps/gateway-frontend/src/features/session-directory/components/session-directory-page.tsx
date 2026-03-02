import {
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiRouteLine,
  RiSunLine,
} from '@remixicon/react'
import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { SessionDirectoryEntry } from '@/features/session-directory/types'

import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { useTheme } from '@/lib/theme'
import { ExpiryStatusBadge, TimestampCell } from '@/components/ui/table-cells'
import { fetchSessionDirectory } from '@/features/session-directory/services/session-directory-api'
import { useServerListController } from '@/lib/use-server-list-controller'

export function SessionDirectoryPage() {
  const { theme, toggleTheme } = useTheme()
  const {
    items: entries,
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
  } = useServerListController(fetchSessionDirectory)

  const columns = useMemo<Array<ColumnDef<SessionDirectoryEntry>>>(
    () => [
      {
        accessorKey: 'sessionId',
        header: 'Session ID',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-muted-foreground">
            {row.original.sessionId}
          </span>
        ),
      },
      {
        accessorKey: 'ownerInstanceId',
        header: 'Owner Instance',
        cell: ({ row }) => (
          <span className="font-mono text-xs">
            {row.original.ownerInstanceId}
          </span>
        ),
      },
      {
        accessorKey: 'wsUrl',
        header: 'WebSocket URL',
        cell: ({ row }) => (
          <span className="inline-block max-w-[250px] truncate text-xs text-muted-foreground">
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
            className="h-7 w-52 text-xs"
            placeholder="Search session, instance, URL..."
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
            onClick={reload}
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
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : entries.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiRouteLine className="size-10 opacity-30" />
            <p className="text-sm">No session directory entries found</p>
          </div>
        ) : (
          <DataTable columns={columns} data={entries} />
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
