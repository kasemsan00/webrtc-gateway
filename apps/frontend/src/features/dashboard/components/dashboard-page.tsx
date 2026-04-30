import {
  RiArrowUpDownLine,
  RiBarChartGroupedLine,
  RiMoonLine,
  RiRefreshLine,
  RiSunLine,
  RiTimeLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import type {
  DashboardPeriod,
  DashboardSummaryResponse,
} from '@/features/dashboard/types'

import Header from '@/components/Header'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { fetchDashboardSummary } from '@/features/dashboard/services/dashboard-api'
import { useTheme } from '@/lib/theme'

// ── Constants ──────────────────────────────────────────────────────────────

const STATE_COLORS: Record<string, string> = {
  ended: '#14b8a6',
  active: '#22c55e',
  failed: '#ef4444',
  connecting: '#f59e0b',
  ringing: '#3b82f6',
  incoming: '#a855f7',
  new: '#6b7280',
  reconnecting: '#f97316',
  unknown: '#9ca3af',
}

const SUCCESS_COLOR = '#22c55e'
const FAILURE_COLOR = '#ef4444'
const LINE_COLOR = '#06b6d4'
const PREV_LINE_COLOR = '#06b6d480'
const BAR_COLOR = '#0ea5e9'

// States considered "successful" vs "failed" for the stacked bar
const SUCCESS_STATES = new Set(['ended', 'active'])
const FAILURE_STATES = new Set(['failed', 'connecting', 'new', 'incoming', 'ringing', 'reconnecting', 'unknown'])

// ── Helpers ────────────────────────────────────────────────────────────────

function getTodayBangkokDate() {
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Bangkok',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
  return formatter.format(new Date())
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}

function formatDuration(seconds: number | undefined | null): string {
  if (seconds == null || !isFinite(seconds)) return '—'
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60)
    const s = Math.round(seconds % 60)
    return `${m}m ${s}s`
  }
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

/** Compute previous period's anchor date by shifting back one period. */
function previousAnchorDate(period: DashboardPeriod, currentAnchor: string): string {
  const d = new Date(currentAnchor + 'T00:00:00+07:00')
  if (period === 'day') d.setDate(d.getDate() - 1)
  else if (period === 'month') d.setMonth(d.getMonth() - 1)
  else d.setFullYear(d.getFullYear() - 1)
  return d.toISOString().slice(0, 10)
}

/** Map session states to success/failure counts. */
function computeSuccessFailure(states: Array<{ state: string; count: number }>) {
  let success = 0
  let failure = 0
  for (const s of states) {
    if (SUCCESS_STATES.has(s.state)) success += s.count
    else if (FAILURE_STATES.has(s.state)) failure += s.count
  }
  return { success, failure }
}

// ── Skeleton Component ─────────────────────────────────────────────────────

function SkeletonCard({ lines = 2 }: { lines?: number }) {
  return (
    <Card className="border-border/60 animate-pulse">
      <CardContent className="space-y-2 p-3">
        <div className="h-3 w-20 rounded bg-muted" />
        {Array.from({ length: lines }).map((_, i) => (
          <div
            key={i}
            className="h-5 rounded bg-muted"
            style={{ width: `${60 + i * 20}%` }}
          />
        ))}
      </CardContent>
    </Card>
  )
}

function SkeletonChart() {
  return (
    <Card className="border-border/60">
      <CardHeader className="pb-0">
        <div className="h-4 w-40 animate-pulse rounded bg-muted" />
      </CardHeader>
      <CardContent className="pt-3">
        <div className="flex h-64 w-full items-center justify-center">
          <div className="h-48 w-full animate-pulse rounded bg-muted/50" />
        </div>
      </CardContent>
    </Card>
  )
}

// ── Custom Tooltips ────────────────────────────────────────────────────────

function LineTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean
  payload?: Array<{ name?: string; value?: number; color?: string; stroke?: string; dataKey?: string }>
  label?: string
}) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div className="rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-md">
      <p className="mb-1 font-medium text-popover-foreground">{label}</p>
      {payload.map((entry, i) => (
        <p key={i} style={{ color: entry.color ?? entry.stroke }}>
          {entry.name ?? entry.dataKey}: {formatNumber(entry.value ?? 0)}
        </p>
      ))}
    </div>
  )
}

function StateTooltip({
  active,
  payload,
}: {
  active?: boolean
  payload?: Array<{ name?: string; value?: number; payload?: { state?: string; count?: number } }>
}) {
  if (!active || !payload || payload.length === 0) return null
  const d = payload[0]?.payload
  return (
    <div className="rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-md">
      <p className="text-popover-foreground">
        <span className="font-medium">{d?.state ?? 'unknown'}</span>: {formatNumber(d?.count ?? 0)}
      </p>
    </div>
  )
}

function BarTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean
  payload?: Array<{ name?: string; value?: number; color?: string }>
  label?: string
}) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div className="rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-md">
      <p className="mb-1 font-medium text-popover-foreground">{label}</p>
      {payload.map((entry, i) => (
        <p key={i} style={{ color: entry.color }}>
          {entry.name}: {formatNumber(entry.value ?? 0)}
        </p>
      ))}
    </div>
  )
}

// ── Main Dashboard Component ───────────────────────────────────────────────

export function DashboardPage() {
  const { theme, toggleTheme } = useTheme()
  const [period, setPeriod] = useState<DashboardPeriod>('day')
  const [anchorDate, setAnchorDate] = useState(getTodayBangkokDate)
  const [summary, setSummary] = useState<DashboardSummaryResponse | null>(null)
  const [prevSummary, setPrevSummary] = useState<DashboardSummaryResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const doLoad = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setError(null)
    try {
      const [data, prevData] = await Promise.all([
        fetchDashboardSummary({ period, anchorDate }),
        fetchDashboardSummary({
          period,
          anchorDate: previousAnchorDate(period, anchorDate),
        }),
      ])
      if (signal?.aborted) return
      setSummary(data)
      setPrevSummary(prevData)
    } catch (err) {
      if (signal?.aborted) return
      setError(
        err instanceof Error ? err.message : 'Failed to load dashboard summary',
      )
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [anchorDate, period])

  const loadSummary = useCallback(() => {
    // Reset 30s timer on manual refresh
    if (intervalRef.current) {
      clearInterval(intervalRef.current)
    }
    const controller = new AbortController()
    void doLoad(controller.signal)
    intervalRef.current = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void doLoad()
      }
    }, 30000)
    return () => controller.abort()
  }, [doLoad])

  // Initial load
  useEffect(() => {
    const cleanup = loadSummary()
    return () => {
      cleanup()
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [loadSummary])

  // Memoized chart data
  const stateChartData = useMemo(() => summary?.states ?? [], [summary?.states])
  const trunkChartData = useMemo(() => summary?.topTrunks ?? [], [summary?.topTrunks])
  const directionData = useMemo(() => summary?.directions ?? [], [summary?.directions])

  // Success / failure data
  const successFailure = useMemo(() => computeSuccessFailure(stateChartData), [stateChartData])

  // Merge current + previous series for comparison chart
  const comparisonSeries = useMemo(() => {
    if (!summary?.series) return []
    const prevMap = new Map<string, number>()
    if (prevSummary?.series) {
      for (const p of prevSummary.series) {
        prevMap.set(p.bucket, p.count)
      }
    }
    return summary.series.map((point) => ({
      bucket: point.bucket,
      current: point.count,
      previous: prevMap.get(point.bucket) ?? 0,
    }))
  }, [summary?.series, prevSummary?.series])

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <select
            className="h-7 rounded-md border border-border bg-background px-2 text-xs"
            value={period}
            onChange={(event) => setPeriod(event.target.value as DashboardPeriod)}
            aria-label="Dashboard period"
          >
            <option value="day">Day</option>
            <option value="month">Month</option>
            <option value="year">Year</option>
          </select>

          <Input
            className="h-7 w-40 text-xs"
            type="date"
            value={anchorDate}
            onChange={(event) => setAnchorDate(event.target.value)}
          />

          <Separator orientation="vertical" className="h-4" />

          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => { void loadSummary() }}
            disabled={loading}
          >
            <RiRefreshLine className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>

          <Separator orientation="vertical" className="h-4" />

          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            onClick={toggleTheme}
            aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            {theme === 'dark' ? <RiSunLine className="size-3.5" /> : <RiMoonLine className="size-3.5" />}
          </Button>
        </div>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        {/* Error banner */}
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
            {summary ? (
              <span className="ml-2 text-yellow-400">(showing cached data)</span>
            ) : null}
          </div>
        ) : null}

        {/* Range indicator */}
        {summary ? (
          <p className="mb-2 text-[11px] text-muted-foreground">
            Range: {new Date(summary.rangeStart).toLocaleString()} — {new Date(summary.rangeEnd).toLocaleString()}
            &nbsp;· Auto-refresh every 30s
          </p>
        ) : null}

        {/* Summary Metrics Cards */}
        {loading && !summary ? (
          <div className="mb-4 grid gap-3 md:grid-cols-2 xl:grid-cols-5">
            {Array.from({ length: 5 }).map((_, i) => (
              <SkeletonCard key={i} lines={i === 2 ? 3 : 2} />
            ))}
          </div>
        ) : summary ? (
          <div className="mb-4 grid gap-3 md:grid-cols-2 xl:grid-cols-5">
            <Card className="border-border/60">
              <CardContent className="space-y-1 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Call Sessions</p>
                <p className="text-lg font-semibold">{formatNumber(summary.metrics.periodSessions)}</p>
              </CardContent>
            </Card>

            <Card className="border-border/60">
              <CardContent className="space-y-1 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Active Sessions</p>
                <p className="text-lg font-semibold">{formatNumber(summary.metrics.activeSessions)}</p>
              </CardContent>
            </Card>

            <Card className="border-border/60">
              <CardContent className="space-y-1 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Trunks</p>
                <p className="text-sm">Registered: {formatNumber(summary.metrics.registeredTrunks)}</p>
                <p className="text-xs text-muted-foreground">
                  Enabled / Total: {formatNumber(summary.metrics.enabledTrunks)} / {formatNumber(summary.metrics.totalTrunks)}
                </p>
              </CardContent>
            </Card>

            <Card className="border-border/60">
              <CardContent className="space-y-1 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Public Accounts</p>
                <p className="text-lg font-semibold">{formatNumber(summary.metrics.publicAccounts)}</p>
              </CardContent>
            </Card>

            <Card className="border-border/60">
              <CardContent className="space-y-1 p-3">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Session Directory</p>
                <p className="text-lg font-semibold">{formatNumber(summary.metrics.sessionDirectoryNow)}</p>
                <p className="text-xs text-muted-foreground">WS Clients: {formatNumber(summary.metrics.wsClients)}</p>
              </CardContent>
            </Card>
          </div>
        ) : null}

        {/* Loading fallback for initial load */}
        {loading && !summary ? (
          <div className="grid gap-4 xl:grid-cols-2">
            <SkeletonChart />
            <SkeletonChart />
            <div className="xl:col-span-2">
              <SkeletonChart />
            </div>
          </div>
        ) : null}

        {/* Charts Section */}
        {summary ? (
          <div className="space-y-4">
            {/* KPI row 2: Duration + Direction + Success Rate */}
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <Card className="border-border/60">
                <CardContent className="space-y-1 p-3">
                  <p className="flex items-center gap-1 text-[11px] uppercase tracking-wide text-muted-foreground">
                    <RiTimeLine className="size-3" />
                    Avg Call Duration
                  </p>
                  <p className="text-lg font-semibold">{formatDuration(summary.metrics.avgDurationSec)}</p>
                  <p className="text-xs text-muted-foreground">
                    Max: {formatDuration(summary.metrics.maxDurationSec)}
                  </p>
                </CardContent>
              </Card>

              <Card className="border-border/60">
                <CardContent className="space-y-1 p-3">
                  <p className="flex items-center gap-1 text-[11px] uppercase tracking-wide text-muted-foreground">
                    <RiArrowUpDownLine className="size-3" />
                    Direction
                  </p>
                  {directionData.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No data</p>
                  ) : (
                    directionData.map((d) => (
                      <p key={d.direction} className="text-sm">
                        <span className="capitalize">{d.direction}</span>: {formatNumber(d.count)}
                      </p>
                    ))
                  )}
                </CardContent>
              </Card>

              <Card className="border-border/60">
                <CardContent className="space-y-1 p-3">
                  <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Success Rate</p>
                  {stateChartData.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No data</p>
                  ) : (
                    <>
                      <p className="text-lg font-semibold text-green-500">
                        {formatNumber(successFailure.success)}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        Failed: {formatNumber(successFailure.failure)}
                        &nbsp;· Rate:{' '}
                        {successFailure.success + successFailure.failure > 0
                          ? `${((successFailure.success / (successFailure.success + successFailure.failure)) * 100).toFixed(1)}%`
                          : 'N/A'}
                      </p>
                    </>
                  )}
                </CardContent>
              </Card>

              <Card className="border-border/60">
                <CardContent className="space-y-1 p-3">
                  <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Previous Period</p>
                  {prevSummary ? (
                    <>
                      <p className="text-lg font-semibold">
                        {formatNumber(prevSummary.metrics.periodSessions)}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {summary.metrics.periodSessions - prevSummary.metrics.periodSessions >= 0 ? '↑' : '↓'}{' '}
                        {formatNumber(Math.abs(summary.metrics.periodSessions - prevSummary.metrics.periodSessions))}
                        &nbsp;(
                        {prevSummary.metrics.periodSessions > 0
                          ? `${((Math.abs(summary.metrics.periodSessions - prevSummary.metrics.periodSessions) / prevSummary.metrics.periodSessions) * 100).toFixed(1)}%`
                          : 'N/A'}
                        )
                      </p>
                    </>
                  ) : (
                    <p className="text-sm text-muted-foreground">No data</p>
                  )}
                </CardContent>
              </Card>
            </div>

            {/* Main chart grid */}
            <div className="grid gap-4 xl:grid-cols-2">
              {/* Sessions Over Time + Previous Period Comparison */}
              <Card className="border-border/60">
                <CardHeader className="pb-0">
                  <CardTitle className="flex items-center gap-2 text-sm">
                    <RiBarChartGroupedLine className="size-4" />
                    Sessions Over Time
                  </CardTitle>
                </CardHeader>
                <CardContent className="pt-3">
                  {summary.series.length === 0 ? (
                    <p className="py-12 text-center text-sm text-muted-foreground">No sessions in selected range</p>
                  ) : (
                    <div className="h-64 w-full">
                      <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={comparisonSeries}>
                          <CartesianGrid strokeDasharray="3 3" strokeOpacity={0.25} />
                          <XAxis dataKey="bucket" tick={{ fontSize: 11 }} />
                          <YAxis allowDecimals={false} tick={{ fontSize: 11 }} />
                          <Tooltip content={<LineTooltip />} />
                          <Legend
                            formatter={(value: string) => (
                              <span className="text-xs text-muted-foreground">{value}</span>
                            )}
                          />
                          <Line
                            type="monotone"
                            dataKey="current"
                            name="Current"
                            stroke={LINE_COLOR}
                            strokeWidth={2}
                            dot={false}
                          />
                          <Line
                            type="monotone"
                            dataKey="previous"
                            name="Previous"
                            stroke={PREV_LINE_COLOR}
                            strokeWidth={1.5}
                            strokeDasharray="4 3"
                            dot={false}
                          />
                        </LineChart>
                      </ResponsiveContainer>
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* Session State Breakdown */}
              <Card className="border-border/60">
                <CardHeader className="pb-0">
                  <CardTitle className="text-sm">Session State Breakdown</CardTitle>
                </CardHeader>
                <CardContent className="pt-3">
                  {stateChartData.length === 0 ? (
                    <p className="py-12 text-center text-sm text-muted-foreground">No state data in selected range</p>
                  ) : (
                    <div className="h-64 w-full">
                      <ResponsiveContainer width="100%" height="100%">
                        <PieChart>
                          <Pie
                            data={stateChartData}
                            dataKey="count"
                            nameKey="state"
                            cx="50%"
                            cy="50%"
                            outerRadius={80}
                            label={({ name, value }: { name?: string; value?: number }) =>
                              `${name} (${value})`
                            }
                          >
                            {stateChartData.map((entry) => (
                              <Cell
                                key={`${entry.state}-${entry.count}`}
                                fill={STATE_COLORS[entry.state] ?? STATE_COLORS.unknown}
                              />
                            ))}
                          </Pie>
                          <Tooltip content={<StateTooltip />} />
                        </PieChart>
                      </ResponsiveContainer>
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* Success / Failure Stacked Bar */}
              <Card className="border-border/60">
                <CardHeader className="pb-0">
                  <CardTitle className="text-sm">Success vs Failure by State</CardTitle>
                </CardHeader>
                <CardContent className="pt-3">
                  {stateChartData.length === 0 ? (
                    <p className="py-12 text-center text-sm text-muted-foreground">No state data in selected range</p>
                  ) : (
                    <div className="h-64 w-full">
                      <ResponsiveContainer width="100%" height="100%">
                        <BarChart
                          data={stateChartData.map((s) => ({
                            name: s.state,
                            count: s.count,
                            fill: SUCCESS_STATES.has(s.state)
                              ? SUCCESS_COLOR
                              : FAILURE_COLOR,
                          }))}
                          layout="vertical"
                        >
                          <CartesianGrid strokeDasharray="3 3" strokeOpacity={0.25} horizontal={false} />
                          <XAxis type="number" tick={{ fontSize: 11 }} />
                          <YAxis type="category" dataKey="name" tick={{ fontSize: 11 }} width={80} />
                          <Tooltip content={<BarTooltip />} />
                          <Bar dataKey="count" radius={[0, 4, 4, 0]}>
                            {stateChartData.map((entry) => (
                              <Cell
                                key={`sf-${entry.state}`}
                                fill={SUCCESS_STATES.has(entry.state) ? SUCCESS_COLOR : FAILURE_COLOR}
                              />
                            ))}
                          </Bar>
                        </BarChart>
                      </ResponsiveContainer>
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* Top Trunks by Call Volume */}
              <Card className="border-border/60 xl:col-span-1">
                <CardHeader className="pb-0">
                  <CardTitle className="text-sm">Top Trunks by Call Volume</CardTitle>
                </CardHeader>
                <CardContent className="pt-3">
                  {trunkChartData.length === 0 ? (
                    <p className="py-12 text-center text-sm text-muted-foreground">No trunk volume data in selected range</p>
                  ) : (
                    <div className="h-64 w-full">
                      <ResponsiveContainer width="100%" height="100%">
                        <BarChart data={trunkChartData} layout="vertical">
                          <CartesianGrid strokeDasharray="3 3" strokeOpacity={0.25} horizontal={false} />
                          <XAxis type="number" tick={{ fontSize: 11 }} />
                          <YAxis type="category" dataKey="trunkName" tick={{ fontSize: 11 }} width={100} />
                          <Tooltip content={<BarTooltip />} />
                          <Bar dataKey="count" fill={BAR_COLOR} radius={[0, 4, 4, 0]} />
                        </BarChart>
                      </ResponsiveContainer>
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  )
}
