import {
  RiFileTextLine,
  RiLoader4Line,
  RiMoonLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useState } from 'react'

import type { LogFile } from '@/features/gateway-logs/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { formatThaiDateTime } from '@/lib/date-time'
import { useTheme } from '@/lib/theme'
import {
  fetchLogFiles,
  fetchLogTail,
} from '@/features/gateway-logs/services/gateway-logs-api'

const TAIL_OPTIONS = [100, 500, 1000, 2000] as const

function formatFileSize(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

export function GatewayLogsPage() {
  const { theme, toggleTheme } = useTheme()
  const [files, setFiles] = useState<Array<LogFile>>([])
  const [selectedName, setSelectedName] = useState<string | null>(null)
  const [tailSize, setTailSize] =
    useState<(typeof TAIL_OPTIONS)[number]>(500)
  const [lines, setLines] = useState<Array<string>>([])
  const [truncated, setTruncated] = useState(false)
  const [loadingFiles, setLoadingFiles] = useState(false)
  const [loadingTail, setLoadingTail] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [autoRefresh, setAutoRefresh] = useState(false)

  const selectedFile = useMemo(
    () => files.find((file) => file.name === selectedName) ?? null,
    [files, selectedName],
  )

  const loadFiles = useCallback(async () => {
    setLoadingFiles(true)
    setError(null)
    try {
      const response = await fetchLogFiles()
      setFiles(response.items)
      if (!selectedName) {
        setSelectedName(
          response.items.find((file) => file.current)?.name ??
            response.items.at(0)?.name ??
            null,
        )
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch log files')
    } finally {
      setLoadingFiles(false)
    }
  }, [selectedName])

  const loadTail = useCallback(async () => {
    setLoadingTail(true)
    setError(null)
    try {
      const response = await fetchLogTail({
        name: selectedName ?? undefined,
        tail: tailSize,
      })
      setLines(response.lines)
      setTruncated(response.truncated)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch log tail')
      setLines([])
    } finally {
      setLoadingTail(false)
    }
  }, [selectedName, tailSize])

  useEffect(() => {
    void loadFiles()
  }, [loadFiles])

  useEffect(() => {
    if (!selectedName) return
    void loadTail()
  }, [selectedName, tailSize, loadTail])

  useEffect(() => {
    if (!autoRefresh) return

    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void loadTail()
      }
    }, 5000)

    return () => clearInterval(timer)
  }, [autoRefresh, loadTail])

  const logText = lines.join('\n')

  const handleCopy = async () => {
    if (!logText) return
    try {
      await navigator.clipboard.writeText(logText)
    } catch {
      setError('Failed to copy log text')
    }
  }

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => {
              void loadFiles()
              void loadTail()
            }}
            disabled={loadingFiles || loadingTail}
          >
            <RiRefreshLine
              className={`size-3.5 ${loadingFiles || loadingTail ? 'animate-spin' : ''}`}
            />
            Refresh
          </Button>
          <Button
            size="sm"
            variant={autoRefresh ? 'secondary' : 'outline'}
            className="h-7 px-2 text-xs"
            onClick={() => setAutoRefresh((value) => !value)}
          >
            Auto 5s
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 text-xs"
            onClick={() => {
              void handleCopy()
            }}
            disabled={!logText}
          >
            Copy
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

      <div className="flex min-h-0 flex-1 gap-3 p-4">
        <aside className="flex w-72 shrink-0 flex-col overflow-hidden rounded-lg border border-border">
          <div className="border-b border-border px-3 py-2 text-sm font-medium">
            Log Files
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {loadingFiles ? (
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
                    onClick={() => setSelectedName(file.name)}
                    className={`w-full rounded-md px-2 py-2 text-left text-xs transition-colors ${
                      selectedName === file.name
                        ? 'bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300'
                        : 'hover:bg-muted'
                    }`}
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
                    <p className="mt-0.5 text-[10px] text-muted-foreground">
                      {formatThaiDateTime(file.modifiedAt)}
                    </p>
                  </button>
                ))}
              </div>
            )}
          </div>
        </aside>

        <section className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border">
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
            <span className="truncate font-mono text-xs text-muted-foreground">
              {selectedFile?.name ?? 'No file selected'}
            </span>
            <div className="ml-auto flex items-center gap-1">
              {TAIL_OPTIONS.map((size) => (
                <Button
                  key={size}
                  size="sm"
                  variant={tailSize === size ? 'secondary' : 'ghost'}
                  className="h-7 px-2 text-xs"
                  onClick={() => setTailSize(size)}
                >
                  {size}
                </Button>
              ))}
            </div>
          </div>

          {error ? (
            <div className="border-b border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
              {error}
            </div>
          ) : null}

          {truncated ? (
            <div className="border-b border-amber-500/30 bg-amber-500/10 px-3 py-1 text-xs text-amber-500">
              Output truncated to last {tailSize} lines.
            </div>
          ) : null}

          <div className="relative min-h-0 flex-1 overflow-auto bg-muted/20 p-3">
            {loadingTail ? (
              <div className="flex justify-center py-10">
                <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
              </div>
            ) : lines.length === 0 ? (
              <p className="text-sm text-muted-foreground">No log lines.</p>
            ) : (
              <pre className="font-mono text-xs leading-5 whitespace-pre-wrap break-all">
                {logText}
              </pre>
            )}
          </div>
        </section>
      </div>
    </div>
  )
}