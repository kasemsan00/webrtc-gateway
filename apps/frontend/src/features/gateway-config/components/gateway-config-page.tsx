import {
  RiCheckLine,
  RiCloseLine,
  RiCodeLine,
  RiFileCopyLine,
  RiFileTextLine,
  RiGridLine,
  RiLoader4Line,
  RiLockLine,
  RiMoonLine,
  RiRefreshLine,
  RiSearchLine,
  RiSettings3Line,
  RiSunLine,
  RiTableLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import type {
  ConfigValueType,
  GatewayConfigResponse,
} from '@/features/gateway-config/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import Header from '@/components/Header'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'
import { fetchGatewayConfig } from '@/features/gateway-config/services/gateway-config-api'
import {
  filterConfigItems,
  flattenConfigSections,
  formatConfigValue,
  getSubsystemCounts,
  toEnvText,
  toJsonText,
} from '@/features/gateway-config/utils/config-utils'

const PREFERRED_SUBSYSTEM_ORDER = [
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
  'observability',
]

function ConfigValueCell({
  value,
  type,
  onCopy,
}: {
  value: unknown
  type: ConfigValueType
  onCopy?: () => void
}) {
  if (type === 'boolean') {
    return (
      <span
        onClick={onCopy}
        className={cn(
          'inline-flex items-center rounded px-1.5 py-0.5 font-mono text-[11px] font-semibold transition-colors cursor-pointer select-none',
          value
            ? 'bg-emerald-500/15 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300'
            : 'bg-muted/70 text-muted-foreground hover:bg-muted',
        )}
        title="Click to copy boolean value"
      >
        {value ? 'true' : 'false'}
      </span>
    )
  }

  if (type === 'secret') {
    return (
      <span
        onClick={onCopy}
        className="inline-flex items-center gap-1 rounded bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] text-amber-600 dark:text-amber-400 cursor-pointer select-none"
        title="Redacted secret (click to copy mask)"
      >
        <RiLockLine className="size-2.5" />
        masked
      </span>
    )
  }

  if (type === 'empty') {
    return (
      <span
        onClick={onCopy}
        className="font-mono text-[11px] text-muted-foreground/50 italic cursor-pointer select-none"
        title="Empty value (click to copy)"
      >
        empty
      </span>
    )
  }

  if (type === 'number') {
    return (
      <span
        onClick={onCopy}
        className="font-mono text-xs font-medium text-sky-600 dark:text-sky-400 cursor-pointer hover:underline select-all"
        title={`Click to copy: ${value}`}
      >
        {String(value)}
      </span>
    )
  }

  const text = formatConfigValue(value)
  return (
    <span
      onClick={onCopy}
      className="max-w-[130px] truncate font-mono text-xs text-foreground cursor-pointer hover:underline select-all sm:max-w-[180px] xl:max-w-[220px]"
      title={`Click to copy: ${text}`}
    >
      {text}
    </span>
  )
}

export function GatewayConfigPage() {
  const { theme, toggleTheme } = useTheme()
  const [config, setConfig] = useState<GatewayConfigResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [selectedSubsystem, setSelectedSubsystem] = useState<string>('all')
  const [viewMode, setViewMode] = useState<'grid' | 'table' | 'raw'>('grid')
  const [rawFormat, setRawFormat] = useState<'env' | 'json'>('env')
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)

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

  const allItems = useMemo(() => {
    return config ? flattenConfigSections(config.sections) : []
  }, [config])

  const subsystemCounts = useMemo(() => {
    return getSubsystemCounts(allItems)
  }, [allItems])

  const subsystems = useMemo(() => {
    const available = Object.keys(subsystemCounts)
    const ordered = PREFERRED_SUBSYSTEM_ORDER.filter((name) =>
      available.includes(name),
    )
    const rest = available
      .filter((name) => !PREFERRED_SUBSYSTEM_ORDER.includes(name))
      .sort((a, b) => a.localeCompare(b))
    return [...ordered, ...rest]
  }, [subsystemCounts])

  const filteredItems = useMemo(() => {
    return filterConfigItems(allItems, search, selectedSubsystem)
  }, [allItems, search, selectedSubsystem])

  const copyToClipboard = useCallback(async (text: string, label?: string) => {
    try {
      await navigator.clipboard.writeText(text)
      if (label) {
        setCopiedKey(label)
        setTimeout(() => {
          setCopiedKey((prev) => (prev === label ? null : prev))
        }, 1500)
      }
      const preview = text.length > 40 ? text.slice(0, 40) + '…' : text
      toast.success(`Copied: ${preview}`)
    } catch {
      toast.error('Failed to copy to clipboard')
    }
  }, [])

  // Shortcut: "/" or "Ctrl+K" to focus search
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (
        (e.key === '/' && document.activeElement?.tagName !== 'INPUT') ||
        ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k')
      ) {
        e.preventDefault()
        searchInputRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

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

      <main className="flex flex-1 flex-col overflow-hidden p-3 sm:p-4">
        <div className="flex flex-col gap-3 min-h-0 flex-1">
          {/* Top Developer Bar: Title, Metadata, Export Actions */}
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-2">
            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-2">
                <RiSettings3Line className="size-5 text-cyan-600 dark:text-cyan-300" />
                <h1 className="text-base font-semibold tracking-tight">
                  Settings
                </h1>
              </div>

              {config ? (
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Separator orientation="vertical" className="h-3.5" />
                  <span>
                    Instance: <span className="font-mono font-medium text-foreground">{config.instanceId}</span>
                  </span>
                  <Separator orientation="vertical" className="h-3.5" />
                  <Badge variant="secondary" className="h-4 px-1.5 text-[10px]">
                    {config.source}
                  </Badge>
                  <Separator orientation="vertical" className="h-3.5" />
                  <span className="font-mono text-muted-foreground">
                    {filteredItems.length === allItems.length ? (
                      `${allItems.length} keys`
                    ) : (
                      `${filteredItems.length} of ${allItems.length} keys`
                    )}
                  </span>
                </div>
              ) : null}
            </div>

            {/* Actions: Copy all & View Mode */}
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="sm"
                className="h-7 px-2 text-xs gap-1"
                onClick={() => void copyToClipboard(toEnvText(filteredItems), 'all-env')}
                disabled={filteredItems.length === 0}
                title="Copy all filtered configuration as .env"
              >
                <RiFileTextLine className="size-3.5" />
                <span className="hidden sm:inline">Copy .env</span>
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-7 px-2 text-xs gap-1"
                onClick={() => void copyToClipboard(toJsonText(filteredItems), 'all-json')}
                disabled={filteredItems.length === 0}
                title="Copy all filtered configuration as JSON"
              >
                <RiFileCopyLine className="size-3.5" />
                <span className="hidden sm:inline">Copy JSON</span>
              </Button>

              <Separator orientation="vertical" className="h-4" />

              {/* View mode switcher */}
              <div className="flex items-center rounded-md border border-border/70 p-0.5 bg-muted/20">
                <Button
                  variant={viewMode === 'grid' ? 'secondary' : 'ghost'}
                  size="sm"
                  className="h-6 px-2 text-xs gap-1"
                  onClick={() => setViewMode('grid')}
                  title="Dense Grid View (Multi-column, compact)"
                >
                  <RiGridLine className="size-3" />
                  <span className="hidden sm:inline">Grid</span>
                </Button>
                <Button
                  variant={viewMode === 'table' ? 'secondary' : 'ghost'}
                  size="sm"
                  className="h-6 px-2 text-xs gap-1"
                  onClick={() => setViewMode('table')}
                  title="Developer Table View"
                >
                  <RiTableLine className="size-3" />
                  <span className="hidden sm:inline">Table</span>
                </Button>
                <Button
                  variant={viewMode === 'raw' ? 'secondary' : 'ghost'}
                  size="sm"
                  className="h-6 px-2 text-xs gap-1"
                  onClick={() => setViewMode('raw')}
                  title="Raw View (.env / JSON)"
                >
                  <RiCodeLine className="size-3" />
                  <span className="hidden sm:inline">Raw</span>
                </Button>
              </div>
            </div>
          </div>

          {/* Search bar & Subsystem chips */}
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <div className="relative flex-1 max-w-md">
                <RiSearchLine className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground pointer-events-none" />
                <Input
                  ref={searchInputRef}
                  className="h-7 pl-8 pr-7 text-xs font-mono bg-background/80"
                  placeholder="Filter key, value, or subsystem... (Press / to search)"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') setSearch('')
                  }}
                />
                {search ? (
                  <button
                    type="button"
                    onClick={() => setSearch('')}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    aria-label="Clear search"
                  >
                    <RiCloseLine className="size-3.5" />
                  </button>
                ) : null}
              </div>

              {/* Quick Subsystem Chips */}
              <div className="flex items-center gap-1 overflow-x-auto py-0.5 no-scrollbar flex-1">
                <button
                  type="button"
                  onClick={() => setSelectedSubsystem('all')}
                  className={cn(
                    'rounded px-2 py-0.5 text-[11px] font-mono transition-colors shrink-0',
                    selectedSubsystem === 'all'
                      ? 'bg-cyan-600/20 text-cyan-700 dark:bg-cyan-600/30 dark:text-cyan-300 font-semibold border border-cyan-500/40'
                      : 'bg-muted/40 text-muted-foreground hover:bg-muted hover:text-foreground border border-transparent',
                  )}
                >
                  All ({allItems.length})
                </button>
                {subsystems.map((subsystem) => (
                  <button
                    key={subsystem}
                    type="button"
                    onClick={() =>
                      setSelectedSubsystem(
                        selectedSubsystem === subsystem ? 'all' : subsystem,
                      )
                    }
                    className={cn(
                      'rounded px-2 py-0.5 text-[11px] font-mono transition-colors shrink-0',
                      selectedSubsystem === subsystem
                        ? 'bg-cyan-600/20 text-cyan-700 dark:bg-cyan-600/30 dark:text-cyan-300 font-semibold border border-cyan-500/40'
                        : 'bg-muted/40 text-muted-foreground hover:bg-muted hover:text-foreground border border-transparent',
                    )}
                  >
                    {subsystem}{' '}
                    <span className="opacity-60 text-[10px]">
                      ({subsystemCounts[subsystem] ?? 0})
                    </span>
                  </button>
                ))}
              </div>
            </div>
          </div>

          {error ? (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
              {error}
            </div>
          ) : null}

          {loading && !config ? (
            <div className="flex items-center gap-2 py-10 justify-center text-sm text-muted-foreground">
              <RiLoader4Line className="size-4 animate-spin" />
              Loading configuration...
            </div>
          ) : null}

          {/* Main content view */}
          <div className="min-h-0 flex-1 overflow-y-auto">
            {filteredItems.length === 0 && !loading ? (
              <div className="flex flex-col items-center justify-center gap-2 py-16 text-muted-foreground">
                <p className="text-sm">No settings found matching your filter.</p>
                {(search || selectedSubsystem !== 'all') && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => {
                      setSearch('')
                      setSelectedSubsystem('all')
                    }}
                  >
                    Clear filters
                  </Button>
                )}
              </div>
            ) : viewMode === 'grid' ? (
              /* Ultra-dense multi-column grid: keys packed closely together */
              <div className="grid grid-cols-1 gap-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 pb-2">
                {filteredItems.map((item) => (
                  <div
                    key={item.fullKey}
                    className={cn(
                      'group flex items-center justify-between gap-1.5 rounded border px-2 py-1 text-xs transition-colors',
                      'border-border/50 bg-card/60 hover:border-cyan-500/50 hover:bg-muted/30',
                      copiedKey === item.fullKey &&
                        'border-emerald-500/60 bg-emerald-500/10',
                    )}
                  >
                    <div className="flex min-w-0 items-center gap-1 max-w-[60%]">
                      <span
                        className="shrink-0 font-mono text-[10px] font-medium text-cyan-600/80 dark:text-cyan-400/80 select-none"
                        title={`Subsystem: ${item.subsystem}`}
                      >
                        {item.subsystem}.
                      </span>
                      <span
                        className="truncate font-mono text-xs font-medium text-foreground hover:underline cursor-pointer"
                        onClick={() =>
                          void copyToClipboard(item.fullKey, item.fullKey)
                        }
                        title={`Click to copy key: ${item.fullKey}`}
                      >
                        {item.key}
                      </span>
                    </div>

                    <div className="flex shrink-0 items-center gap-1.5">
                      <ConfigValueCell
                        value={item.value}
                        type={item.type}
                        onCopy={() =>
                          void copyToClipboard(
                            formatConfigValue(item.value),
                            item.fullKey,
                          )
                        }
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-5 p-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 hover:text-foreground"
                        onClick={() =>
                          void copyToClipboard(
                            `${item.fullKey}=${formatConfigValue(item.value)}`,
                            item.fullKey,
                          )
                        }
                        title={`Copy ${item.fullKey}=${formatConfigValue(item.value)}`}
                      >
                        {copiedKey === item.fullKey ? (
                          <RiCheckLine className="size-3 text-emerald-500" />
                        ) : (
                          <RiFileCopyLine className="size-3" />
                        )}
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            ) : viewMode === 'table' ? (
              /* Developer Table View */
              <div className="rounded-md border border-border/60 bg-card/40 overflow-hidden">
                <Table className="text-xs font-mono">
                  <TableHeader className="sticky top-0 bg-muted/80 backdrop-blur z-10">
                    <TableRow className="h-7 border-b border-border/70">
                      <TableHead className="py-1 px-2 font-semibold">Key</TableHead>
                      <TableHead className="py-1 px-2 font-semibold w-24">Subsystem</TableHead>
                      <TableHead className="py-1 px-2 font-semibold w-20">Type</TableHead>
                      <TableHead className="py-1 px-2 font-semibold">Value</TableHead>
                      <TableHead className="py-1 px-2 font-semibold w-16 text-right">Copy</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filteredItems.map((item) => (
                      <TableRow
                        key={item.fullKey}
                        className="h-7 hover:bg-muted/40 border-b border-border/40"
                      >
                        <TableCell className="py-1 px-2 font-medium">
                          <span
                            className="hover:underline cursor-pointer"
                            onClick={() => void copyToClipboard(item.fullKey, item.fullKey)}
                            title="Click to copy key name"
                          >
                            <span className="text-cyan-600/80 dark:text-cyan-400/80">{item.subsystem}.</span>
                            <span className="text-foreground">{item.key}</span>
                          </span>
                        </TableCell>
                        <TableCell className="py-1 px-2">
                          <Badge variant="outline" className="h-4 px-1 text-[10px]">
                            {item.subsystem}
                          </Badge>
                        </TableCell>
                        <TableCell className="py-1 px-2 text-muted-foreground text-[11px]">
                          {item.type}
                        </TableCell>
                        <TableCell className="py-1 px-2">
                          <ConfigValueCell
                            value={item.value}
                            type={item.type}
                            onCopy={() =>
                              void copyToClipboard(formatConfigValue(item.value), item.fullKey)
                            }
                          />
                        </TableCell>
                        <TableCell className="py-1 px-2 text-right">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="size-5 p-0 text-muted-foreground hover:text-foreground"
                            onClick={() =>
                              void copyToClipboard(
                                `${item.fullKey}=${formatConfigValue(item.value)}`,
                                item.fullKey,
                              )
                            }
                            title="Copy key=value"
                          >
                            {copiedKey === item.fullKey ? (
                              <RiCheckLine className="size-3 text-emerald-500" />
                            ) : (
                              <RiFileCopyLine className="size-3" />
                            )}
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              /* Raw View (.env or JSON) */
              <div className="flex flex-col gap-2 h-full">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-1 rounded border border-border p-0.5 bg-muted/20">
                    <Button
                      variant={rawFormat === 'env' ? 'secondary' : 'ghost'}
                      size="sm"
                      className="h-6 px-2 text-xs"
                      onClick={() => setRawFormat('env')}
                    >
                      .env format
                    </Button>
                    <Button
                      variant={rawFormat === 'json' ? 'secondary' : 'ghost'}
                      size="sm"
                      className="h-6 px-2 text-xs"
                      onClick={() => setRawFormat('json')}
                    >
                      JSON format
                    </Button>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 px-2 text-xs gap-1"
                    onClick={() =>
                      void copyToClipboard(
                        rawFormat === 'env'
                          ? toEnvText(filteredItems)
                          : toJsonText(filteredItems),
                      )
                    }
                  >
                    <RiFileCopyLine className="size-3" />
                    Copy raw content
                  </Button>
                </div>
                <pre className="flex-1 rounded-md border border-border/70 bg-muted/20 p-3 font-mono text-xs leading-relaxed text-foreground overflow-auto">
                  {rawFormat === 'env'
                    ? toEnvText(filteredItems)
                    : toJsonText(filteredItems)}
                </pre>
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  )
}
