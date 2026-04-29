import {
  RiLoader4Line,
  RiMoonLine,
  RiPhoneLine,
  RiRefreshLine,
  RiSunLine,
  RiTranslate2,
} from '@remixicon/react'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { ActiveSession } from '@/features/active-sessions/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { useTheme } from '@/lib/theme'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import {
  fetchActiveSessions,
  subscribeSessionEvents,
} from '@/features/active-sessions/services/active-sessions-api'

function formatDuration(seconds: number): string {
  const mins = Math.floor(seconds / 60)
  const secs = seconds % 60
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

export function ActiveSessionsPage() {
  const { theme, toggleTheme } = useTheme()
  const [sessions, setSessions] = useState<Array<ActiveSession>>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchActiveSessions()
      setSessions(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch sessions')
    } finally {
      setLoading(false)
    }
  }, [])

  const loadRef = useRef(load)
  loadRef.current = load

  useEffect(() => {
    void load()
  }, [load])

  const handleReload = useCallback(() => {
    void loadRef.current()
  }, [])

  useVisibilityRealtimeReload({
    subscribe: subscribeSessionEvents,
    onReload: handleReload,
  })

  const columns: Array<ColumnDef<ActiveSession>> = [
    {
      accessorKey: 'id',
      header: 'Session ID',
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.id}</span>
      ),
    },
    {
      accessorKey: 'state',
      header: 'State',
      cell: ({ row }) => {
        const state = row.original.state
        let variant: 'default' | 'success' | 'warning' | 'destructive' =
          'default'
        if (state === 'active') variant = 'success'
        else if (state === 'connecting' || state === 'ringing')
          variant = 'warning'
        return (
          <Badge variant={variant} className="text-[10px]">
            {state}
          </Badge>
        )
      },
    },
    {
      accessorKey: 'direction',
      header: 'Direction',
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {row.original.direction}
        </span>
      ),
    },
    {
      accessorKey: 'from',
      header: 'From',
      cell: ({ row }) => (
        <span className="text-xs">{row.original.from || '-'}</span>
      ),
    },
    {
      accessorKey: 'to',
      header: 'To',
      cell: ({ row }) => (
        <span className="text-xs">{row.original.to || '-'}</span>
      ),
    },
    {
      accessorKey: 'authMode',
      header: 'Auth Mode',
      cell: ({ row }) => {
        const mode = row.original.authMode
        return (
          <Badge variant="outline" className="text-[10px]">
            {mode || '-'}
          </Badge>
        )
      },
    },
    {
      accessorKey: 'trunkName',
      header: 'Trunk',
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {row.original.trunkName || '-'}
        </span>
      ),
    },
    {
      accessorKey: 'durationSec',
      header: 'Duration',
      cell: ({ row }) => (
        <span className="font-mono text-xs">
          {formatDuration(row.original.durationSec)}
        </span>
      ),
    },
    {
      accessorKey: 'translatorEnabled',
      header: 'Translator',
      cell: ({ row }) => {
        const enabled = row.original.translatorEnabled
        if (!enabled) return <span className="text-xs text-muted-foreground">-</span>
        return (
          <Badge
            variant="success"
            className="flex w-fit items-center gap-1 text-[10px]"
            title={`${row.original.translatorSrcLang ?? '?'} → ${row.original.translatorTgtLang ?? '?'}`}
          >
            <RiTranslate2 className="size-3" />
            {row.original.translatorSrcLang ?? '?'}→{row.original.translatorTgtLang ?? '?'}
          </Badge>
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
        ) : sessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiPhoneLine className="size-10 opacity-30" />
            <p className="text-sm">No active sessions</p>
          </div>
        ) : (
          <DataTable columns={columns} data={sessions} />
        )}
      </div>
    </div>
  )
}
