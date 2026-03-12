import {
  RiAccountCircleLine,
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { PublicAccount } from '@/features/public-accounts/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { formatThaiDateTime } from '@/lib/date-time'
import { useTheme } from '@/lib/theme'
import { fetchPublicAccounts } from '@/features/public-accounts/services/public-accounts-api'

const formatShortDate = (isoString: string) =>
  formatThaiDateTime(isoString, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })

export function PublicAccountsPage() {
  const { theme, toggleTheme } = useTheme()
  const [accounts, setAccounts] = useState<Array<PublicAccount>>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchPublicAccounts()
      setAccounts(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch accounts')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
    const pollTimer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void load()
      }
    }, 30000)

    return () => {
      clearInterval(pollTimer)
    }
  }, [load])

  const columns: Array<ColumnDef<PublicAccount>> = [
    {
      accessorKey: 'key',
      header: 'Account Key',
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.key}</span>
      ),
    },
    {
      accessorKey: 'isRegistered',
      header: 'Status',
      cell: ({ row }) => {
        const registered = row.original.isRegistered
        const expired = new Date(row.original.expiresAt) < new Date()
        let variant: 'default' | 'success' | 'warning' | 'destructive' =
          'default'
        let label = 'Unknown'
        if (registered && !expired) {
          variant = 'success'
          label = 'Registered'
        } else if (expired) {
          variant = 'destructive'
          label = 'Expired'
        } else {
          variant = 'warning'
          label = 'Not Registered'
        }
        return (
          <Badge variant={variant} className="text-[10px]">
            {label}
          </Badge>
        )
      },
    },
    {
      accessorKey: 'domain',
      header: 'Domain',
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {row.original.domain}
        </span>
      ),
    },
    {
      accessorKey: 'port',
      header: 'Port',
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.port}</span>
      ),
    },
    {
      accessorKey: 'username',
      header: 'Username',
      cell: ({ row }) => (
        <span className="text-xs">{row.original.username}</span>
      ),
    },
    {
      accessorKey: 'refCountActiveCalls',
      header: 'Active Calls',
      cell: ({ row }) => {
        const count = row.original.refCountActiveCalls
        return (
          <Badge
            variant={count > 0 ? 'default' : 'outline'}
            className="text-[10px]"
          >
            {count}
          </Badge>
        )
      },
    },
    {
      accessorKey: 'lastUsedAt',
      header: 'Last Used',
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {formatShortDate(row.original.lastUsedAt)}
        </span>
      ),
    },
    {
      accessorKey: 'expiresAt',
      header: 'Expires',
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {formatShortDate(row.original.expiresAt)}
        </span>
      ),
    },
    {
      accessorKey: 'lastError',
      header: 'Last Error',
      cell: ({ row }) => {
        const err = row.original.lastError
        if (!err)
          return <span className="text-xs text-muted-foreground">-</span>
        return (
          <span className="text-xs text-red-400" title={err}>
            {err.length > 30 ? `${err.substring(0, 30)}...` : err}
          </span>
        )
      },
    },
  ]

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
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : accounts.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiAccountCircleLine className="size-10 opacity-30" />
            <p className="text-sm">No public accounts registered</p>
          </div>
        ) : (
          <DataTable columns={columns} data={accounts} />
        )}
      </div>
    </div>
  )
}
