import {
  RiArrowDownLine,
  RiDownloadLine,
  RiFileCopyLine,
  RiFileTextLine,
  RiLoader4Line,
  RiPauseLine,
  RiPlayLine,
  RiRefreshLine,
} from '@remixicon/react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import type { LogFile } from '@/features/gateway-logs/types'
import type { LevelFilter, LogLevel } from '@/features/gateway-logs/log-lines'
import type { LogsSearch } from '@/lib/search-params'

import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { copyText } from '@/components/ui/table-cells'
import {
  filterLogLines,
  highlightParts,
  parseLogLines,
} from '@/features/gateway-logs/log-lines'
import {
  fetchLogFiles,
  fetchLogTail,
} from '@/features/gateway-logs/services/gateway-logs-api'
import { formatRelativeTime, formatThaiDateTime } from '@/lib/date-time'
import { useNow } from '@/lib/use-now'
import { cn } from '@/lib/utils'

/** The gateway caps a tail request at 5000 lines (logger.MaxTailLines). */
const TAIL_OPTIONS = [500, 2000, 5000] as const
const REFRESH_INTERVALS = [2, 5, 10] as const

const LEVEL_CLASS: Record<LogLevel, string> = {
  error: 'bg-rose-500/10 text-rose-700 dark:text-rose-300',
  warn: 'bg-amber-500/10 text-amber-800 dark:text-amber-200',
  ok: 'text-emerald-700 dark:text-emerald-300',
  info: '',
}

const SELECT_CLASS =
  'h-7 rounded-md border border-border bg-background px-2 text-xs text-foreground'

function formatFileSize(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

export function GatewayLogsPage() {
  const search = useSearch({ from: '/logs' })
  const navigate = useNavigate({ from: '/logs' })
  const [files, setFiles] = useState<Array<LogFile>>([])
  const [selectedName, setSelectedName] = useState<string | null>(
    search.file ?? null,
  )
  // A deep link with a search term loads the maximum tail so the match is found.
  const [tailSize, setTailSize] = useState<(typeof TAIL_OPTIONS)[number]>(
    search.q ? 5000 : 500,
  )
  const [lines, setLines] = useState<Array<string>>([])
  const [truncated, setTruncated] = useState(false)
  const [filesLoading, setFilesLoading] = useState(true)
  const [tailLoading, setTailLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [refreshIntervalSec, setRefreshIntervalSec] =
    useState<(typeof REFRESH_INTERVALS)[number]>(5)
  const [query, setQuery] = useState(search.q ?? '')
  const [level, setLevel] = useState<LevelFilter>('all')
  const [component, setComponent] = useState('')
  const [fetchedAt, setFetchedAt] = useState<number | null>(null)
  const [atBottom, setAtBottom] = useState(true)
  const logScrollRef = useRef<HTMLDivElement>(null)
  const stickToBottomRef = useRef(true)
  const didAutoEnableLiveRef = useRef(false)
  const now = useNow(5000)

  useEffect(() => {
    setQuery(search.q ?? '')
  }, [search.q])

  const selectedFile = useMemo(
    () => files.find((file) => file.name === selectedName) ?? null,
    [files, selectedName],
  )

  const loadFiles = useCallback(async () => {
    try {
      const response = await fetchLogFiles()
      setFiles(response.items)
      setSelectedName(
        (current) =>
          current ??
          response.items.find((file) => file.current)?.name ??
          response.items.at(0)?.name ??
          null,
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch log files')
    } finally {
      setFilesLoading(false)
    }
  }, [])

  // Refreshes never blank the view: previous lines stay until new ones arrive.
  const loadTail = useCallback(async () => {
    if (!selectedName) return
    setRefreshing(true)
    try {
      const response = await fetchLogTail({
        name: selectedName,
        tail: tailSize,
      })
      setLines(response.lines)
      setTruncated(response.truncated)
      setFetchedAt(Date.now())
      setError(null)
      if (stickToBottomRef.current) {
        requestAnimationFrame(() => {
          const el = logScrollRef.current
          if (el) el.scrollTop = el.scrollHeight
        })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch log tail')
    } finally {
      setRefreshing(false)
      setTailLoading(false)
    }
  }, [selectedName, tailSize])

  useEffect(() => {
    void loadFiles()
  }, [loadFiles])

  useEffect(() => {
    void loadTail()
  }, [loadTail])

  useEffect(() => {
    if (!autoRefresh) return
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') void loadTail()
    }, refreshIntervalSec * 1000)
    return () => clearInterval(timer)
  }, [autoRefresh, loadTail, refreshIntervalSec])

  // Follow the current log by default, unless the page was opened to search.
  useEffect(() => {
    if (didAutoEnableLiveRef.current || !selectedFile) return
    didAutoEnableLiveRef.current = true
    if (selectedFile.current && !search.q) setAutoRefresh(true)
  }, [search.q, selectedFile])

  const parsed = useMemo(() => parseLogLines(lines), [lines])
  const components = useMemo(() => {
    const counts = new Map<string, number>()
    for (const line of parsed) {
      if (line.component) {
        counts.set(line.component, (counts.get(line.component) ?? 0) + 1)
      }
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1])
  }, [parsed])
  const levelCounts = useMemo(() => {
    let errors = 0
    let warnings = 0
    for (const line of parsed) {
      if (line.level === 'error') errors += 1
      else if (line.level === 'warn') warnings += 1
    }
    return { errors, warnings }
  }, [parsed])
  const visible = useMemo(
    () =>
      filterLogLines(parsed, {
        query,
        level,
        component: component || undefined,
      }),
    [component, level, parsed, query],
  )
  const filtering =
    Boolean(query.trim()) || level !== 'all' || Boolean(component)
  const visibleText = visible.map((line) => line.text).join('\n')

  const updateQueryParam = (value: string) => {
    void navigate({
      search: (prev: LogsSearch) => ({ ...prev, q: value.trim() || undefined }),
      replace: true,
    })
  }

  const selectFile = (name: string) => {
    setSelectedName(name)
    setTailLoading(true)
    void navigate({
      search: (prev: LogsSearch) => ({ ...prev, file: name }),
      replace: true,
    })
  }

  const handleDownload = () => {
    const blob = new Blob([visibleText], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${selectedName ?? 'gateway'}${filtering ? '.filtered' : ''}.log`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  const scrollToBottom = () => {
    const el = logScrollRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
    stickToBottomRef.current = true
    setAtBottom(true)
  }

  const toolbar = (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <select
        className={cn(SELECT_CLASS, 'max-w-[220px] lg:hidden')}
        value={selectedName ?? ''}
        onChange={(e) => selectFile(e.target.value)}
        aria-label="Log file"
      >
        {files.map((file) => (
          <option key={file.name} value={file.name}>
            {file.name}
            {file.current ? ' (current)' : ''}
          </option>
        ))}
      </select>
      <Input
        className="h-7 w-full text-xs sm:w-64"
        placeholder="Filter: session ID, Call-ID, trunk, any text…"
        aria-label="Filter log lines"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onBlur={(e) => updateQueryParam(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') updateQueryParam(e.currentTarget.value)
        }}
      />
      <div
        className="flex items-center gap-0.5"
        role="group"
        aria-label="Level"
      >
        {(
          [
            ['all', 'All lines', parsed.length],
            [
              'problems',
              'Warn + error',
              levelCounts.errors + levelCounts.warnings,
            ],
            ['errors', 'Errors', levelCounts.errors],
          ] as const
        ).map(([value, label, count]) => (
          <Button
            key={value}
            size="sm"
            variant={level === value ? 'secondary' : 'ghost'}
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setLevel(value)}
            aria-pressed={level === value}
          >
            {label}
            <span className="tabular-nums text-muted-foreground">{count}</span>
          </Button>
        ))}
      </div>
      <select
        className={SELECT_CLASS}
        value={component}
        onChange={(e) => setComponent(e.target.value)}
        aria-label="Component"
      >
        <option value="">All components</option>
        {components.map(([name, count]) => (
          <option key={name} value={name}>
            {name} ({count})
          </option>
        ))}
      </select>
      <span className="ml-auto flex items-center gap-1">
        <span className="text-muted-foreground">Last</span>
        {TAIL_OPTIONS.map((size) => (
          <Button
            key={size}
            size="sm"
            variant={tailSize === size ? 'secondary' : 'ghost'}
            className="h-7 px-1.5 text-xs"
            onClick={() => setTailSize(size)}
          >
            {size >= 1000 ? `${size / 1000}k` : size}
          </Button>
        ))}
        <span className="text-muted-foreground">lines</span>
      </span>
    </div>
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header subBar={toolbar} subBarAlways>
        <Button
          size="sm"
          variant={autoRefresh ? 'secondary' : 'outline'}
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => setAutoRefresh((value) => !value)}
          aria-pressed={autoRefresh}
          title={
            autoRefresh
              ? 'Stop following the log'
              : 'Re-fetch the tail on an interval'
          }
        >
          {autoRefresh ? (
            <RiPauseLine className="size-3.5" />
          ) : (
            <RiPlayLine className="size-3.5" />
          )}
          {autoRefresh ? 'Following' : 'Follow'}
        </Button>
        {autoRefresh ? (
          <select
            className={SELECT_CLASS}
            value={refreshIntervalSec}
            onChange={(e) =>
              setRefreshIntervalSec(
                Number(e.target.value) as (typeof REFRESH_INTERVALS)[number],
              )
            }
            aria-label="Follow interval"
          >
            {REFRESH_INTERVALS.map((sec) => (
              <option key={sec} value={sec}>
                every {sec}s
              </option>
            ))}
          </select>
        ) : null}
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() => {
            void loadFiles()
            void loadTail()
          }}
          disabled={refreshing}
        >
          <RiRefreshLine
            className={cn('size-3.5', refreshing && 'animate-spin')}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={() =>
            void copyText(visibleText, `${visible.length} lines copied`)
          }
          disabled={!visible.length}
          title="Copy the lines currently shown"
        >
          <RiFileCopyLine className="size-3.5" />
          <span className="hidden sm:inline">Copy</span>
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 px-2 text-xs"
          onClick={handleDownload}
          disabled={!visible.length}
          title="Download the lines currently shown"
        >
          <RiDownloadLine className="size-3.5" />
          <span className="hidden sm:inline">Download</span>
        </Button>
      </Header>

      <div className="flex min-h-0 flex-1 gap-3 p-4">
        <aside className="hidden w-64 shrink-0 flex-col overflow-hidden rounded-lg border border-border lg:flex">
          <div className="border-b border-border px-3 py-2 text-sm font-medium">
            Log files
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {filesLoading ? (
              <div className="flex justify-center py-8">
                <RiLoader4Line className="size-5 animate-spin text-muted-foreground" />
              </div>
            ) : files.length === 0 ? (
              <p className="px-2 py-4 text-xs text-muted-foreground">
                No log files found.
              </p>
            ) : (
              <div className="space-y-1">
                {files.map((file) => (
                  <button
                    key={file.name}
                    type="button"
                    onClick={() => selectFile(file.name)}
                    className={cn(
                      'w-full rounded-md px-2 py-2 text-left text-xs transition-colors',
                      selectedName === file.name
                        ? 'bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300'
                        : 'hover:bg-muted',
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <RiFileTextLine className="size-3.5 shrink-0" />
                      <span className="truncate font-mono">{file.name}</span>
                    </div>
                    <div className="mt-1 flex items-center gap-2 text-[10px] text-muted-foreground">
                      <span>{formatFileSize(file.size)}</span>
                      {file.current ? (
                        <Badge variant="success" className="text-[10px]">
                          current
                        </Badge>
                      ) : null}
                    </div>
                    <p
                      className="mt-0.5 text-[10px] text-muted-foreground"
                      title={formatThaiDateTime(file.modifiedAt)}
                    >
                      modified {formatRelativeTime(file.modifiedAt, now)}
                    </p>
                  </button>
                ))}
              </div>
            )}
          </div>
        </aside>

        <section className="relative flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border px-3 py-1.5 text-[11px] text-muted-foreground">
            <span className="truncate font-mono">
              {selectedFile?.name ?? 'No file selected'}
            </span>
            <span>
              {filtering
                ? `${visible.length} of ${parsed.length} lines match`
                : `${parsed.length} lines`}
            </span>
            {levelCounts.errors ? (
              <button
                type="button"
                className="text-rose-600 hover:underline dark:text-rose-400"
                onClick={() => setLevel('errors')}
              >
                {levelCounts.errors} error line
                {levelCounts.errors === 1 ? '' : 's'}
              </button>
            ) : null}
            {truncated ? (
              <span className="text-amber-600 dark:text-amber-400">
                only the last {tailSize.toLocaleString()} lines are loaded
                {tailSize < 5000
                  ? '; load more lines to see older entries'
                  : ''}
              </span>
            ) : null}
            <span className="ml-auto">
              {fetchedAt ? `fetched ${formatRelativeTime(fetchedAt, now)}` : ''}
              {autoRefresh ? ` · following every ${refreshIntervalSec}s` : ''}
            </span>
          </div>

          {error ? (
            <div className="border-b border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
              {error}
              {lines.length ? ' · showing the last loaded lines' : ''}
            </div>
          ) : null}

          <div
            ref={logScrollRef}
            className="min-h-0 flex-1 overflow-auto bg-muted/20 py-2"
            onScroll={() => {
              const el = logScrollRef.current
              if (!el) return
              const nearBottom =
                el.scrollHeight - el.scrollTop - el.clientHeight < 48
              stickToBottomRef.current = nearBottom
              setAtBottom(nearBottom)
            }}
          >
            {tailLoading ? (
              <div className="flex justify-center py-10">
                <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
              </div>
            ) : visible.length === 0 ? (
              <p className="px-3 text-sm text-muted-foreground">
                {parsed.length
                  ? 'No line matches the filter. Load more lines or clear the filter.'
                  : 'No log lines.'}
              </p>
            ) : (
              <table className="w-full border-collapse font-mono text-[11px] leading-5">
                <tbody>
                  {visible.map((line) => (
                    <tr key={line.index} className={LEVEL_CLASS[line.level]}>
                      <td className="w-12 select-none px-2 text-right align-top text-muted-foreground/60">
                        {line.index + 1}
                      </td>
                      <td className="whitespace-pre-wrap break-all pr-3">
                        {highlightParts(line.text, query).map((part, i) =>
                          part.match ? (
                            <mark
                              key={i}
                              className="rounded-sm bg-yellow-300/70 px-0.5 text-foreground dark:bg-yellow-500/40"
                            >
                              {part.text}
                            </mark>
                          ) : (
                            <span key={i}>{part.text}</span>
                          ),
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
          {!atBottom && visible.length > 0 ? (
            <Button
              size="sm"
              variant="secondary"
              className="absolute bottom-3 right-4 h-7 gap-1 px-2 text-xs shadow"
              onClick={scrollToBottom}
            >
              <RiArrowDownLine className="size-3.5" />
              Jump to latest
            </Button>
          ) : null}
        </section>
      </div>
    </div>
  )
}
