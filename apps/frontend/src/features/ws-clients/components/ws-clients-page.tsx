// apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx
import {
  RiLoader4Line,
  RiMoonLine,
  RiPhoneLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  fetchWSClients,
  subscribeWSClientEvents,
} from '../services/ws-clients-api'
import type { ColumnDef } from '@tanstack/react-table'

import type { WSClient, WSClientStreamEvent } from '../types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { useTheme } from '@/lib/theme'
import {
  hangupSession,
  sendSessionDtmf,
} from '@/features/active-sessions/services/session-control-api'

const DTMF_PATTERN = /^[0-9*#]+$/
const DTMF_MAX_LEN = 32
const ACTIVE_CALL_STATES = new Set(['incall', 'connecting', 'ringing'])

export function WSClientsPage() {
  const { theme, toggleTheme } = useTheme()
  const [clients, setClients] = useState<Array<WSClient>>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)
  const [dtmfClient, setDtmfClient] = useState<WSClient | null>(null)
  const [dtmfDigits, setDtmfDigits] = useState('')
  const [dtmfSubmitting, setDtmfSubmitting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchWSClients()
      setClients(data)
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Failed to fetch WS clients',
      )
    } finally {
      setLoading(false)
    }
  }, [])

  const loadRef = useRef(load)
  loadRef.current = load

  useEffect(() => {
    void load()
  }, [load])

  const handleSseEvent = useCallback((event: WSClientStreamEvent) => {
    setClients((prev) => {
      if (event.type === 'disconnected') {
        return prev.filter((c) => c.clientId !== event.clientId)
      }
      if (event.client) {
        const idx = prev.findIndex((c) => c.clientId === event.clientId)
        if (idx === -1) return [...prev, event.client]
        const next = [...prev]
        next[idx] = event.client
        return next
      }
      return prev
    })
  }, [])

  // Single SSE subscription (pre-flight resolution: no useVisibilityRealtimeReload — SSE persists)
  useEffect(() => {
    const unsubscribe = subscribeWSClientEvents(handleSseEvent, () => {
      void loadRef.current()
    })
    return unsubscribe
  }, [handleSseEvent])

  const handleHangup = useCallback(async (sessionId: string) => {
    if (
      !window.confirm(
        `Hang up session ${sessionId}? This sends SIP BYE via the gateway.`,
      )
    ) {
      return
    }
    setActionError(null)
    setActionLoadingId(sessionId)
    try {
      await hangupSession(sessionId)
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : 'Failed to hang up session',
      )
    } finally {
      setActionLoadingId(null)
    }
  }, [])

  const handleOpenDtmf = useCallback((client: WSClient) => {
    setActionError(null)
    setDtmfDigits('')
    setDtmfClient(client)
  }, [])

  const handleSubmitDtmf = useCallback(async () => {
    if (!dtmfClient?.sessionId) return
    const digits = dtmfDigits.trim()
    if (!digits || !DTMF_PATTERN.test(digits) || digits.length > DTMF_MAX_LEN) {
      setActionError('DTMF must be 1–32 characters: digits 0-9, * or # only')
      return
    }
    setActionError(null)
    setDtmfSubmitting(true)
    try {
      await sendSessionDtmf(dtmfClient.sessionId, digits)
      setDtmfClient(null)
      setDtmfDigits('')
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to send DTMF')
    } finally {
      setDtmfSubmitting(false)
    }
  }, [dtmfClient, dtmfDigits])

  const columns = useMemo<Array<ColumnDef<WSClient>>>(
    () => [
      {
        accessorKey: 'clientId',
        header: 'Client ID',
        cell: ({ row }) => (
          <span className="font-mono text-xs">
            {row.original.clientId.slice(0, 8)}…
          </span>
        ),
      },
      {
        accessorKey: 'sessionId',
        header: 'Session',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-muted-foreground">
            {row.original.sessionId || '-'}
          </span>
        ),
      },
      {
        id: 'trunk',
        header: 'Trunk',
        cell: ({ row }) => {
          const c = row.original
          if (!c.trunkResolved) {
            return (
              <Badge variant="outline" className="text-[10px]">
                unresolved
              </Badge>
            )
          }
          const label = c.resolvedTrunkPublicId || `#${c.resolvedTrunkId}`
          return (
            <Badge variant="success" className="text-[10px] font-mono">
              {label}
            </Badge>
          )
        },
      },
      {
        accessorKey: 'availability',
        header: 'Availability',
        cell: ({ row }) => {
          const a = row.original.availability
          let variant: 'default' | 'success' | 'warning' | 'destructive' =
            'default'
          if (a === 'idle') variant = 'success'
          else if (a === 'busy') variant = 'warning'
          else if (a === 'unavailable') variant = 'destructive'
          return (
            <Badge variant={variant} className="text-[10px]">
              {a || '-'}
            </Badge>
          )
        },
      },
      {
        accessorKey: 'callState',
        header: 'Call State',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.callState || '-'}</span>
        ),
      },
      {
        accessorKey: 'authSubject',
        header: 'Auth',
        cell: ({ row }) => (
          <span className="truncate text-xs text-muted-foreground">
            {row.original.authSubject || '-'}
          </span>
        ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        cell: ({ row }) => {
          const c = row.original
          const isActive =
            !!c.sessionId && ACTIVE_CALL_STATES.has(c.callState ?? '')
          if (!isActive) return null
          const busy = actionLoadingId === c.sessionId
          return (
            <div className="flex justify-end gap-1">
              <Button
                size="sm"
                variant="secondary"
                className="h-6 px-2 text-[10px]"
                onClick={() => handleOpenDtmf(c)}
              >
                DTMF
              </Button>
              <Button
                size="sm"
                variant="destructive"
                className="h-6 px-2 text-[10px]"
                disabled={busy}
                onClick={() => void handleHangup(c.sessionId!)}
              >
                {busy ? '…' : 'Hangup'}
              </Button>
            </div>
          )
        },
      },
    ],
    [actionLoadingId, handleHangup, handleOpenDtmf],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => void load()}
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
        {actionError ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {actionError}
          </div>
        ) : null}
        {loading && clients.length === 0 ? (
          <div className="flex justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : clients.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiPhoneLine className="size-10 opacity-30" />
            <p className="text-sm">No connected WebSocket clients</p>
          </div>
        ) : (
          <DataTable columns={columns} data={clients} />
        )}
      </div>

      <Dialog
        open={dtmfClient !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDtmfClient(null)
            setDtmfDigits('')
          }
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Send DTMF</DialogTitle>
            <DialogDescription className="font-mono text-xs">
              Session {dtmfClient?.sessionId}
            </DialogDescription>
          </DialogHeader>
          <Input
            value={dtmfDigits}
            onChange={(e) => setDtmfDigits(e.target.value)}
            placeholder="e.g. 123#"
            className="font-mono text-sm"
            maxLength={DTMF_MAX_LEN}
          />
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDtmfClient(null)}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={dtmfSubmitting}
              onClick={() => void handleSubmitDtmf()}
            >
              {dtmfSubmitting ? 'Sending…' : 'Send'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
