import {
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSettings3Line,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useState } from 'react'

import type { GatewayConfigResponse } from '@/features/gateway-config/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { useTheme } from '@/lib/theme'
import { fetchGatewayConfig } from '@/features/gateway-config/services/gateway-config-api'

const SECTION_LABELS: Record<string, string> = {
  turn: 'TURN',
  sip: 'SIP',
  api: 'API',
  auth: 'Auth',
  rtp: 'RTP',
  db: 'Database',
  sipPublic: 'SIP Public Accounts',
  sipTrunk: 'SIP Trunks',
  gateway: 'Gateway Instance',
  sessionDir: 'Session Directory',
  push: 'Push Notifications',
  translator: 'Translator',
}

const SECTION_ORDER = [
  'gateway',
  'api',
  'auth',
  'sip',
  'rtp',
  'turn',
  'db',
  'sipTrunk',
  'sipPublic',
  'sessionDir',
  'push',
  'translator',
]

function formatLabel(key: string) {
  return key
    .replace(/([A-Z])/g, ' $1')
    .replace(/_/g, ' ')
    .trim()
}

function formatValue(value: unknown) {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function isMaskedValue(value: unknown) {
  return value === '****'
}

function ConfigValueCell({ value }: { value: unknown }) {
  if (typeof value === 'boolean') {
    return (
      <Badge variant={value ? 'success' : 'secondary'} className="text-[10px]">
        {value ? 'enabled' : 'disabled'}
      </Badge>
    )
  }

  if (isMaskedValue(value)) {
    return (
      <span className="font-mono text-xs text-muted-foreground">••••••••</span>
    )
  }

  const text = formatValue(value)
  return (
    <span className="break-all font-mono text-xs text-foreground">{text}</span>
  )
}

function ConfigSectionCard({
  sectionKey,
  values,
}: {
  sectionKey: string
  values: Record<string, unknown>
}) {
  const entries = Object.entries(values).sort(([a], [b]) => a.localeCompare(b))
  const title = SECTION_LABELS[sectionKey] ?? formatLabel(sectionKey)

  return (
    <Card className="border-border/70">
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No values configured.</p>
        ) : (
          entries.map(([key, value]) => (
            <div
              key={key}
              className="grid grid-cols-1 gap-1 border-b border-border/50 py-2 last:border-b-0 sm:grid-cols-[minmax(0,1.2fr)_minmax(0,2fr)] sm:gap-4"
            >
              <span className="text-xs font-medium text-muted-foreground">
                {formatLabel(key)}
              </span>
              <ConfigValueCell value={value} />
            </div>
          ))
        )}
      </CardContent>
    </Card>
  )
}

export function GatewayConfigPage() {
  const { theme, toggleTheme } = useTheme()
  const [config, setConfig] = useState<GatewayConfigResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchGatewayConfig()
      setConfig(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch config')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const orderedSections = useMemo(() => {
    if (!config) return []

    const known = SECTION_ORDER.filter((key) => key in config.sections).map(
      (key) => [key, config.sections[key]] as const,
    )
    const extra = Object.entries(config.sections)
      .filter(([key]) => !SECTION_ORDER.includes(key))
      .sort(([a], [b]) => a.localeCompare(b))

    return [...known, ...extra]
  }, [config])

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => toggleTheme()}
          >
            {theme === 'dark' ? (
              <RiSunLine className="size-3.5" />
            ) : (
              <RiMoonLine className="size-3.5" />
            )}
            <span className="hidden sm:inline">Theme</span>
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => {
              void load()
            }}
            disabled={loading}
          >
            {loading ? (
              <RiLoader4Line className="size-3.5 animate-spin" />
            ) : (
              <RiRefreshLine className="size-3.5" />
            )}
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>
      </Header>

      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl space-y-4 p-4">
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="mb-1 flex items-center gap-2">
              <RiSettings3Line className="size-5 text-cyan-600 dark:text-cyan-300" />
              <h1 className="text-xl font-semibold">Gateway Settings</h1>
            </div>
            <p className="text-sm text-muted-foreground">
              Read-only view of effective configuration loaded from environment
              variables at startup.
            </p>
          </div>
        </div>

        {error ? (
          <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        ) : null}

        {config ? (
          <div className="rounded-md border border-border/70 bg-muted/20 px-3 py-2 text-sm">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <span>
                <span className="text-muted-foreground">Instance:</span>{' '}
                <span className="font-mono">{config.instanceId}</span>
              </span>
              <Separator orientation="vertical" className="hidden h-4 sm:block" />
              <span>
                <span className="text-muted-foreground">Source:</span>{' '}
                <Badge variant="secondary" className="text-[10px]">
                  {config.source}
                </Badge>
              </span>
            </div>
          </div>
        ) : null}

        {loading && !config ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <RiLoader4Line className="size-4 animate-spin" />
            Loading configuration...
          </div>
        ) : null}

        <div className="grid gap-4 lg:grid-cols-2">
          {orderedSections.map(([sectionKey, values]) => (
            <ConfigSectionCard
              key={sectionKey}
              sectionKey={sectionKey}
              values={values}
            />
          ))}
        </div>
        </div>
      </main>
    </div>
  )
}
