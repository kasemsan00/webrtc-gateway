import {
  RiAccountCircleLine,
  RiLoader4Line,
  RiRefreshLine,
} from '@remixicon/react'
import { Link, useSearch } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { PublicAccount } from '@/features/public-accounts/types'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { IdCell, TimestampCell } from '@/components/ui/table-cells'
import { fetchPublicAccounts } from '@/features/public-accounts/services/public-accounts-api'
import { formatRelativeTime } from '@/lib/date-time'
import { useNow } from '@/lib/use-now'
import { cn } from '@/lib/utils'

export function publicAccountStatus(
  account: Pick<PublicAccount, 'isRegistered' | 'expiresAt' | 'lastError'>,
  now: number = Date.now(),
): {
  label: string
  variant: 'success' | 'warning' | 'destructive' | 'secondary'
  hint: string
} {
  const expiresMs = Date.parse(account.expiresAt)
  const expired = !Number.isNaN(expiresMs) && expiresMs < now
  if (account.isRegistered && !expired) {
    return {
      label: 'Registered',
      variant: 'success',
      hint: 'Registered with the SIP server; guests can call through it.',
    }
  }
  if (expired) {
    return {
      label: 'Expired',
      variant: 'secondary',
      hint: 'Idle past its lifetime; it is re-registered on the next guest call.',
    }
  }
  if (account.lastError) {
    return {
      label: 'Failed',
      variant: 'destructive',
      hint: `Registration failed: ${account.lastError}`,
    }
  }
  return {
    label: 'Not registered',
    variant: 'warning',
    hint: 'Not registered yet.',
  }
}

export function PublicAccountsPage() {
  const routeSearch = useSearch({ from: '/public-accounts' })
  const [accounts, setAccounts] = useState<Array<PublicAccount>>([])
  const [initialLoading, setInitialLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState(routeSearch.search ?? '')
  const [fetchedAt, setFetchedAt] = useState<number | null>(null)
  const now = useNow(5000)

  const load = useCallback(async () => {
    setRefreshing(true)
    try {
      setAccounts(await fetchPublicAccounts())
      setFetchedAt(Date.now())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch accounts')
    } finally {
      setRefreshing(false)
      setInitialLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
    const pollTimer = setInterval(() => {
      if (document.visibilityState === 'visible') void load()
    }, 30000)
    return () => clearInterval(pollTimer)
  }, [load])

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return accounts
    return accounts.filter((a) =>
      [a.key, a.username, a.domain, a.lastError].some((v) =>
        v.toLowerCase().includes(q),
      ),
    )
  }, [accounts, query])

  const counts = useMemo(() => {
    let registered = 0
    let failed = 0
    let inCall = 0
    for (const a of accounts) {
      const status = publicAccountStatus(a, now)
      if (status.label === 'Registered') registered += 1
      if (status.label === 'Failed') failed += 1
      if (a.refCountActiveCalls > 0) inCall += 1
    }
    return { registered, failed, inCall }
  }, [accounts, now])

  const columns = useMemo<Array<ColumnDef<PublicAccount>>>(
    () => [
      {
        id: 'account',
        header: 'SIP account',
        cell: ({ row }) => (
          <div className="text-xs">
            <p className="font-mono">
              {row.original.username}@{row.original.domain}:{row.original.port}
            </p>
            <IdCell
              value={row.original.key}
              label="Account key"
              maxWidth="max-w-[160px]"
            />
          </div>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: ({ row }) => {
          const status = publicAccountStatus(row.original, now)
          return (
            <Badge
              variant={status.variant}
              className="text-[10px]"
              title={status.hint}
            >
              {status.label}
            </Badge>
          )
        },
      },
      {
        accessorKey: 'refCountActiveCalls',
        header: 'Live calls',
        cell: ({ row }) => {
          const count = row.original.refCountActiveCalls
          return (
            <span
              className={cn(
                'text-xs tabular-nums',
                count > 0 && 'font-semibold',
              )}
            >
              {count}
            </span>
          )
        },
      },
      {
        accessorKey: 'lastUsedAt',
        header: 'Last used',
        cell: ({ row }) => (
          <TimestampCell value={row.original.lastUsedAt} relative now={now} />
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
        accessorKey: 'lastError',
        header: 'Last error',
        cell: ({ row }) => {
          const err = row.original.lastError
          if (!err)
            return <span className="text-xs text-muted-foreground">-</span>
          return (
            <span
              className="inline-block max-w-[260px] truncate font-mono text-[11px] text-rose-600 dark:text-rose-400"
              title={err}
            >
              {err}
            </span>
          )
        },
      },
      {
        id: 'logs',
        header: () => <div className="text-right">Logs</div>,
        cell: ({ row }) => (
          <div className="text-right">
            <Link
              to="/logs"
              search={{ q: row.original.username }}
              className="text-xs text-cyan-700 hover:underline dark:text-cyan-400"
            >
              logs
            </Link>
          </div>
        ),
      },
    ],
    [now],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <Input
          className="h-7 w-40 text-xs sm:w-56"
          placeholder="Search user, domain, error…"
          aria-label="Search public accounts"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => void load()}
          disabled={refreshing}
        >
          <RiRefreshLine
            className={cn('size-3.5', refreshing && 'animate-spin')}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        <p className="mb-3 max-w-3xl text-xs text-muted-foreground">
          Public (guest) callers do not have their own SIP account. The gateway
          registers a shared account on demand, counts the calls using it, and
          lets it expire when idle.
        </p>
        <div className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            <strong className="text-foreground tabular-nums">
              {accounts.length}
            </strong>{' '}
            accounts
          </span>
          <span>{counts.registered} registered</span>
          {counts.failed ? (
            <span className="text-rose-600 dark:text-rose-400">
              {counts.failed} failed
            </span>
          ) : null}
          <span>{counts.inCall} in use</span>
          <span className="ml-auto">
            {fetchedAt
              ? `fetched ${formatRelativeTime(fetchedAt, now)} · every 30s`
              : ''}
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
        ) : accounts.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiAccountCircleLine className="size-10 opacity-30" />
            <p className="text-sm">No public accounts registered</p>
            <p className="text-xs">
              One appears when a public caller starts a call.
            </p>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={visible}
            emptyMessage="No account matches the search."
          />
        )}
      </div>
    </div>
  )
}
