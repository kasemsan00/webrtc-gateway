import { Link } from '@tanstack/react-router'
import {
  RiArrowDownLine,
  RiArrowRightLine,
  RiArrowUpDownLine,
  RiArrowUpLine,
  RiBarChartGroupedLine,
  RiCheckboxCircleLine,
  RiDatabaseLine,
  RiFileTextLine,
  RiHistoryLine,
  RiInboxLine,
  RiInformationLine,
  RiMoonLine,
  RiPulseLine,
  RiRefreshLine,
  RiRouteLine,
  RiServerLine,
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
import type React from 'react'

import type {
  DashboardPeriod,
  DashboardSummaryMetrics,
  DashboardSummaryResponse,
} from '@/features/dashboard/types'

import type { GatewayDashboard } from '@/features/gateway-instances/types'
import Header from '@/components/Header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  Tooltip as UITooltip,
} from '@/components/ui/tooltip'
import {
  fetchDashboardSummary,
  fetchGatewayHealth,
} from '@/features/dashboard/services/dashboard-api'
import {
  buildTerminalOutcomeChartData,
  buildTerminalTrunkRows,
  summarizeTerminalOutcomes,
} from '@/features/dashboard/terminal-outcomes'
import { cn } from '@/lib/utils'
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
const IN_PROGRESS_COLOR = '#f59e0b'
const LINE_COLOR = '#06b6d4'
const PREV_LINE_COLOR = '#06b6d480'
const BAR_COLOR = '#0ea5e9'

const SUCCESS_STATES = new Set(['ended', 'active'])
const FAILURE_STATES = new Set(['failed', 'unknown'])
const IN_PROGRESS_STATES = new Set([
  'connecting',
  'new',
  'incoming',
  'ringing',
  'reconnecting',
])

type Accent = 'cyan' | 'emerald' | 'amber' | 'violet' | 'sky' | 'rose' | 'slate'

const ACCENT_ICON: Record<Accent, string> = {
  cyan: 'text-cyan-500 dark:text-cyan-400',
  emerald: 'text-emerald-500 dark:text-emerald-400',
  amber: 'text-amber-500 dark:text-amber-400',
  violet: 'text-violet-500 dark:text-violet-400',
  sky: 'text-sky-500 dark:text-sky-400',
  rose: 'text-rose-500 dark:text-rose-400',
  slate: 'text-slate-500 dark:text-slate-400',
}

const ACCENT_BORDER: Record<Accent, string> = {
  cyan: 'border-l-cyan-500/50',
  emerald: 'border-l-emerald-500/50',
  amber: 'border-l-amber-500/50',
  violet: 'border-l-violet-500/50',
  sky: 'border-l-sky-500/50',
  rose: 'border-l-rose-500/50',
  slate: 'border-l-slate-500/50',
}

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

function formatBangkokDateTime(iso: string) {
  return new Date(iso).toLocaleString('en-GB', {
    timeZone: 'Asia/Bangkok',
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

function getStateOutcomeColor(state: string): string {
  if (SUCCESS_STATES.has(state)) return SUCCESS_COLOR
  if (FAILURE_STATES.has(state)) return FAILURE_COLOR
  if (IN_PROGRESS_STATES.has(state)) return IN_PROGRESS_COLOR
  return STATE_COLORS[state] ?? STATE_COLORS.unknown
}

function previousAnchorDate(
  period: DashboardPeriod,
  currentAnchor: string,
): string {
  const d = new Date(currentAnchor + 'T00:00:00+07:00')
  if (period === 'day') d.setDate(d.getDate() - 1)
  else if (period === 'month') d.setMonth(d.getMonth() - 1)
  else d.setFullYear(d.getFullYear() - 1)
  return d.toISOString().slice(0, 10)
}

function computeSuccessFailure(
  states: Array<{ state: string; count: number }>,
) {
  let success = 0
  let failure = 0
  for (const s of states) {
    if (SUCCESS_STATES.has(s.state)) success += s.count
    else if (FAILURE_STATES.has(s.state)) failure += s.count
  }
  return { success, failure }
}

function computeSuccessRate(success: number, failure: number): number | null {
  const total = success + failure
  if (total === 0) return null
  return (success / total) * 100
}

function computeSystemStatus(m: DashboardSummaryMetrics | undefined): {
  label: string
  tone: 'success' | 'warning' | 'danger' | 'muted'
  pulse: boolean
} {
  if (!m) return { label: 'Loading', tone: 'muted', pulse: false }
  if (m.totalTrunks === 0 && m.publicAccounts === 0) {
    return { label: 'Unconfigured', tone: 'danger', pulse: false }
  }
  if (m.activeSessions > 0) {
    return { label: 'Active', tone: 'success', pulse: true }
  }
  if (m.registeredTrunks > 0 || m.publicAccounts > 0) {
    return { label: 'Operational', tone: 'success', pulse: false }
  }
  return { label: 'Idle', tone: 'warning', pulse: false }
}

const TONE: Record<
  'success' | 'warning' | 'danger' | 'muted',
  { dot: string; text: string }
> = {
  success: {
    dot: 'bg-emerald-500',
    text: 'text-emerald-600 dark:text-emerald-400',
  },
  warning: {
    dot: 'bg-amber-500',
    text: 'text-amber-600 dark:text-amber-400',
  },
  danger: {
    dot: 'bg-rose-500',
    text: 'text-rose-600 dark:text-rose-400',
  },
  muted: {
    dot: 'bg-muted-foreground',
    text: 'text-muted-foreground',
  },
}

// ── Small Components ───────────────────────────────────────────────────────

function TrendBadge({
  current,
  previous,
  invert = false,
  neutral = false,
}: {
  current: number
  previous?: number
  invert?: boolean
  neutral?: boolean
}) {
  if (previous == null || !isFinite(previous)) return null
  const delta = current - previous
  const isFlat = delta === 0
  if (isFlat && current === 0) return null

  const isUp = delta > 0
  const isDown = delta < 0
  const pct =
    previous > 0 ? Math.abs((delta / previous) * 100) : current > 0 ? 100 : 0

  let colorClass: string
  if (isFlat) {
    colorClass = 'text-muted-foreground'
  } else if (neutral) {
    colorClass = 'text-muted-foreground'
  } else {
    const good = invert ? isDown : isUp
    colorClass = good
      ? 'text-emerald-600 dark:text-emerald-400'
      : 'text-rose-600 dark:text-rose-400'
  }

  if (isFlat) {
    return (
      <span className="inline-flex items-center gap-0.5 text-xs font-medium text-muted-foreground">
        — 0%
      </span>
    )
  }

  const Icon = isUp ? RiArrowUpLine : RiArrowDownLine
  return (
    <UITooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            'inline-flex cursor-default items-center gap-0.5 text-xs font-medium',
            colorClass,
          )}
        >
          <Icon className="size-3" />
          {pct.toFixed(1)}%
        </span>
      </TooltipTrigger>
      <TooltipContent>
        vs previous: {formatNumber(previous)} → {formatNumber(current)}
      </TooltipContent>
    </UITooltip>
  )
}

function KpiCard({
  icon,
  label,
  value,
  sub,
  accent = 'slate',
  trend,
  progress,
  to,
  hint,
}: {
  icon: React.ReactNode
  label: string
  value: React.ReactNode
  sub?: React.ReactNode
  accent?: Accent
  trend?: {
    current: number
    previous?: number
    invert?: boolean
    neutral?: boolean
  }
  progress?: number
  to?: string
  hint?: string
}) {
  const card = (
    <Card
      className={cn(
        'border-l-2 transition-all hover:-translate-y-0.5 hover:shadow-sm',
        ACCENT_BORDER[accent],
        to && 'cursor-pointer hover:border-l-opacity-80',
      )}
    >
      <CardContent className="p-4">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-1">
            <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
              {label}
            </p>
            {hint ? (
              <UITooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground"
                    onClick={(e) => e.preventDefault()}
                  >
                    <RiInformationLine className="size-3" />
                  </button>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">{hint}</TooltipContent>
              </UITooltip>
            ) : null}
          </div>
          <span className={cn('shrink-0', ACCENT_ICON[accent])}>{icon}</span>
        </div>
        <p className="mt-2 text-2xl font-semibold tabular-nums leading-none">
          {value}
        </p>
        {sub ? (
          <div className="mt-1.5 text-xs text-muted-foreground">{sub}</div>
        ) : null}
        {progress != null ? (
          <Progress value={progress} className="mt-3 h-1.5" />
        ) : null}
        {trend ? (
          <div className="mt-2">
            <TrendBadge
              current={trend.current}
              previous={trend.previous}
              invert={trend.invert}
              neutral={trend.neutral}
            />
          </div>
        ) : null}
      </CardContent>
    </Card>
  )

  if (to) {
    return (
      <Link to={to} className="block">
        {card}
      </Link>
    )
  }

  return card
}

function SectionHeading({
  icon,
  title,
  actionTo,
  actionLabel,
}: {
  icon: React.ReactNode
  title: string
  actionTo?: string
  actionLabel?: string
}) {
  return (
    <div className="flex items-center gap-3">
      <span className="text-muted-foreground">{icon}</span>
      <h2 className="text-sm font-semibold uppercase tracking-wide text-foreground">
        {title}
      </h2>
      <div className="h-px flex-1 bg-border/60" />
      {actionTo && actionLabel ? (
        <Button
          asChild
          variant="ghost"
          size="xs"
          className="shrink-0 gap-1 text-xs text-muted-foreground"
        >
          <Link to={actionTo}>
            <span>{actionLabel}</span>
            <RiArrowRightLine className="size-3" />
          </Link>
        </Button>
      ) : null}
    </div>
  )
}

function EmptyState({
  icon,
  message,
}: {
  icon: React.ReactNode
  message: string
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
      <span className="opacity-40">{icon}</span>
      <p className="text-xs">{message}</p>
    </div>
  )
}

function KpiSkeleton() {
  return (
    <Card className="border-l-2 border-l-transparent">
      <CardContent className="p-4">
        <div className="flex items-start justify-between">
          <Skeleton className="h-3 w-20" />
          <Skeleton className="size-4 rounded" />
        </div>
        <Skeleton className="mt-3 h-7 w-24" />
        <Skeleton className="mt-2 h-3 w-16" />
      </CardContent>
    </Card>
  )
}

function ChartSkeleton() {
  return (
    <Card>
      <CardHeader className="pb-0">
        <Skeleton className="h-4 w-40" />
      </CardHeader>
      <CardContent className="pt-3">
        <Skeleton className="h-64 w-full" />
      </CardContent>
    </Card>
  )
}

function formatUptimeSeconds(seconds: number) {
  const total = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const remain = total % 60
  return `${hours}h ${minutes}m ${remain}s`
}

function HealthStripSkeleton() {
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      <Skeleton className="h-8 w-full" />
      <Skeleton className="h-8 w-full" />
      <Skeleton className="h-8 w-full" />
    </div>
  )
}

function HealthStrip({ health }: { health: GatewayDashboard }) {
  return (
    <div className="flex flex-col gap-3 text-xs sm:flex-row sm:items-center sm:justify-between">
      <div className="grid flex-1 gap-3 sm:grid-cols-3">
        <div>
          <p className="text-muted-foreground">Instance</p>
          <p className="font-mono">{health.instanceId}</p>
        </div>
        <div>
          <p className="text-muted-foreground">Uptime</p>
          <p>{formatUptimeSeconds(health.uptimeSeconds)}</p>
        </div>
        <div>
          <p className="text-muted-foreground">Database</p>
          <Badge variant={health.dbConnected ? 'success' : 'destructive'}>
            {health.dbConnected ? 'Connected' : 'Unavailable'}
          </Badge>
        </div>
      </div>
      <Button asChild variant="outline" size="sm" className="h-7 shrink-0 text-xs">
        <Link to="/instances">View instances</Link>
      </Button>
    </div>
  )
}

// ── System Status Hero ──────────────────────────────────────────────────────

function SystemStatusHero({
  summary,
  prevSummary,
  health,
  healthLoading,
}: {
  summary: DashboardSummaryResponse | null
  prevSummary: DashboardSummaryResponse | null
  health: GatewayDashboard | null
  healthLoading: boolean
}) {
  const status = computeSystemStatus(summary?.metrics)
  const tone = TONE[status.tone]
  const active = summary?.metrics.activeSessions ?? 0
  const period = summary?.metrics.periodSessions ?? 0
  const prevPeriod = prevSummary?.metrics.periodSessions

  return (
    <Card className="overflow-hidden">
      <div className="bg-linear-to-r from-cyan-600/10 via-emerald-600/5 to-transparent dark:from-cyan-500/15 dark:via-emerald-500/10">
        <CardContent className="flex flex-col gap-4 p-5 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:gap-5">
            <div className="flex items-center gap-2.5">
              <span className="relative flex size-3 shrink-0">
                {status.pulse ? (
                  <span
                    className={cn(
                      'absolute inline-flex h-full w-full animate-ping rounded-full opacity-60',
                      tone.dot,
                    )}
                  />
                ) : null}
                <span
                  className={cn(
                    'relative inline-flex size-3 rounded-full',
                    tone.dot,
                  )}
                />
              </span>
              <div>
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                  System Status
                </p>
                <p className={cn('text-base font-semibold', tone.text)}>
                  {status.label}
                </p>
              </div>
            </div>

            <Separator
              orientation="vertical"
              className="hidden h-12 sm:block"
            />

            <div>
              <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                Active Sessions
              </p>
              <div className="flex items-baseline gap-2">
                <p className="text-3xl font-bold tabular-nums leading-none">
                  {formatNumber(active)}
                </p>
                {status.pulse ? (
                  <Badge variant="success" className="gap-1">
                    <RiPulseLine className="size-3 animate-pulse" />
                    live
                  </Badge>
                ) : null}
              </div>
            </div>

            <Separator
              orientation="vertical"
              className="hidden h-12 sm:block"
            />

            <div>
              <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                Period Sessions
              </p>
              <div className="flex items-center gap-2">
                <p className="text-3xl font-bold tabular-nums leading-none">
                  {formatNumber(period)}
                </p>
                <TrendBadge current={period} previous={prevPeriod} />
              </div>
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button
              asChild
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
            >
              <Link to="/active-sessions">
                <RiPulseLine className="size-3.5" />
                Active
              </Link>
            </Button>
            <Button
              asChild
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
            >
              <Link to="/trunks">
                <RiServerLine className="size-3.5" />
                Trunks
              </Link>
            </Button>
            <Button
              asChild
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
            >
              <Link to="/sessions">
                <RiHistoryLine className="size-3.5" />
                History
              </Link>
            </Button>
            <Button
              asChild
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
            >
              <Link to="/logs">
                <RiFileTextLine className="size-3.5" />
                Logs
              </Link>
            </Button>
          </div>
        </CardContent>
      </div>
      {healthLoading || health ? (
        <div className="border-t border-border/60 px-5 py-3">
          {healthLoading && !health ? (
            <HealthStripSkeleton />
          ) : health ? (
            <HealthStrip health={health} />
          ) : null}
        </div>
      ) : null}
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
  payload?: Array<{
    name?: string
    value?: number
    color?: string
    stroke?: string
    dataKey?: string
  }>
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
  payload?: Array<{
    name?: string
    value?: number
    payload?: { state?: string; count?: number }
  }>
}) {
  if (!active || !payload || payload.length === 0) return null
  const d = payload[0]?.payload
  return (
    <div className="rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-md">
      <p className="text-popover-foreground">
        <span className="font-medium">{d?.state ?? 'unknown'}</span>:{' '}
        {formatNumber(d?.count ?? 0)}
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
  const [prevSummary, setPrevSummary] =
    useState<DashboardSummaryResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const [health, setHealth] = useState<GatewayDashboard | null>(null)
  const [healthLoading, setHealthLoading] = useState(true)
  const [healthError, setHealthError] = useState<string | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadHealth = useCallback(async () => {
    setHealthLoading(true)
    try {
      const data = await fetchGatewayHealth()
      setHealth(data)
      setHealthError(null)
    } catch (err) {
      setHealthError(
        err instanceof Error ? err.message : 'Failed to load gateway health',
      )
    } finally {
      setHealthLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadHealth()
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void loadHealth()
      }
    }, 30000)
    return () => clearInterval(timer)
  }, [loadHealth])

  const doLoad = useCallback(
    async (signal?: AbortSignal) => {
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
        setLastUpdated(Date.now())
      } catch (err) {
        if (signal?.aborted) return
        setError(
          err instanceof Error
            ? err.message
            : 'Failed to load dashboard summary',
        )
      } finally {
        if (!signal?.aborted) setLoading(false)
      }
    },
    [anchorDate, period],
  )

  const loadSummary = useCallback(() => {
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

  useEffect(() => {
    const cleanup = loadSummary()
    return () => {
      cleanup()
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [loadSummary])

  // Memoized chart data
  const stateChartData = useMemo(() => summary?.states ?? [], [summary?.states])
  const trunkChartData = useMemo(
    () => summary?.topTrunks ?? [],
    [summary?.topTrunks],
  )
  const directionData = useMemo(
    () => summary?.directions ?? [],
    [summary?.directions],
  )
  const terminalOutcomeSummary = useMemo(
    () => summarizeTerminalOutcomes(summary?.terminalOutcomes ?? []),
    [summary?.terminalOutcomes],
  )
  const terminalOutcomeChartData = useMemo(
    () => buildTerminalOutcomeChartData(summary?.terminalOutcomes ?? []),
    [summary?.terminalOutcomes],
  )
  const terminalTrunkRows = useMemo(
    () => buildTerminalTrunkRows(summary?.terminalTrunks ?? []),
    [summary?.terminalTrunks],
  )

  const successFailure = useMemo(
    () => computeSuccessFailure(stateChartData),
    [stateChartData],
  )
  const successRate = useMemo(
    () => computeSuccessRate(successFailure.success, successFailure.failure),
    [successFailure],
  )

  const prevSuccessFailure = useMemo(
    () => computeSuccessFailure(prevSummary?.states ?? []),
    [prevSummary?.states],
  )
  const prevSuccessRate = useMemo(
    () =>
      computeSuccessRate(
        prevSuccessFailure.success,
        prevSuccessFailure.failure,
      ),
    [prevSuccessFailure],
  )

  const inboundCount = useMemo(
    () => directionData.find((d) => d.direction === 'inbound')?.count ?? 0,
    [directionData],
  )
  const outboundCount = useMemo(
    () => directionData.find((d) => d.direction === 'outbound')?.count ?? 0,
    [directionData],
  )

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

  const showSkeletons = loading && !summary
  const lastUpdatedLabel = lastUpdated
    ? new Date(lastUpdated).toLocaleTimeString()
    : null

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex flex-wrap items-center justify-end gap-2 text-xs">
          <Tabs
            value={period}
            onValueChange={(v) => setPeriod(v as DashboardPeriod)}
          >
            <TabsList className="h-7">
              <TabsTrigger value="day" className="text-xs">
                Day
              </TabsTrigger>
              <TabsTrigger value="month" className="text-xs">
                Month
              </TabsTrigger>
              <TabsTrigger value="year" className="text-xs">
                Year
              </TabsTrigger>
            </TabsList>
          </Tabs>

          <Input
            className="hidden h-7 w-36 text-xs sm:block md:w-40"
            type="date"
            value={anchorDate}
            onChange={(event) => setAnchorDate(event.target.value)}
          />

          <Separator orientation="vertical" className="hidden h-4 sm:block" />

          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => {
              void loadSummary()
            }}
            disabled={loading}
          >
            <RiRefreshLine
              className={`size-3.5 ${loading ? 'animate-spin' : ''}`}
            />
            <span className="hidden sm:inline">Refresh</span>
          </Button>

          <Separator orientation="vertical" className="hidden h-4 sm:block" />

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

      <TooltipProvider>
        <div className="flex-1 overflow-y-auto p-4 md:p-6">
          {/* Error banner */}
          {error ? (
            <div className="mb-4 flex items-center gap-2 rounded-md border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-sm text-rose-500 dark:text-rose-400">
              <RiInformationLine className="size-4 shrink-0" />
              <span>{error}</span>
              {summary ? (
                <span className="ml-1 text-amber-500">
                  (showing cached data)
                </span>
              ) : null}
            </div>
          ) : null}

          {/* Range + last updated indicator */}
          {summary ? (
            <p className="mb-4 text-[11px] text-muted-foreground">
              Range: {formatBangkokDateTime(summary.rangeStart)} —{' '}
              {formatBangkokDateTime(summary.rangeEnd)}
              {' · Asia/Bangkok (ICT)'}
              {lastUpdatedLabel ? ` · Updated ${lastUpdatedLabel}` : ''}
              {' · Auto-refresh every 30s'}
            </p>
          ) : null}

          {healthError ? (
            <div className="mb-4 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-600 dark:text-amber-400">
              Gateway health: {healthError}
            </div>
          ) : null}

          {error && !summary && !showSkeletons ? (
            <div className="flex flex-col items-center justify-center gap-4 py-20 text-center">
              <RiInboxLine className="size-10 text-muted-foreground/40" />
              <div className="space-y-1">
                <p className="text-sm font-medium">Failed to load dashboard</p>
                <p className="max-w-md text-xs text-muted-foreground">{error}</p>
              </div>
              <Button
                size="sm"
                variant="outline"
                className="gap-1.5"
                onClick={() => {
                  void loadSummary()
                }}
                disabled={loading}
              >
                <RiRefreshLine
                  className={`size-3.5 ${loading ? 'animate-spin' : ''}`}
                />
                Retry
              </Button>
            </div>
          ) : (
            <>
          {/* System Status Hero */}
          {showSkeletons ? (
            <Card className="mb-6 overflow-hidden">
              <CardContent className="p-5">
                <div className="flex items-center gap-5">
                  <Skeleton className="h-10 w-40" />
                  <Skeleton className="h-10 w-32" />
                  <Skeleton className="h-10 w-32" />
                </div>
                <div className="mt-4 border-t border-border/60 pt-3">
                  <HealthStripSkeleton />
                </div>
              </CardContent>
            </Card>
          ) : (
            <div className="mb-6">
              <SystemStatusHero
                summary={summary}
                prevSummary={prevSummary}
                health={health}
                healthLoading={healthLoading}
              />
            </div>
          )}

          {/* ── Overview Section ── */}
          <section className="mb-8 space-y-3">
            <SectionHeading
              icon={<RiBarChartGroupedLine className="size-4" />}
              title="Overview"
            />

            {showSkeletons ? (
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
                {Array.from({ length: 5 }).map((_, i) => (
                  <KpiSkeleton key={i} />
                ))}
              </div>
            ) : summary ? (
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
                <KpiCard
                  icon={<RiHistoryLine className="size-4" />}
                  label="Call Sessions"
                  accent="cyan"
                  to="/sessions"
                  value={formatNumber(summary.metrics.periodSessions)}
                  trend={{
                    current: summary.metrics.periodSessions,
                    previous: prevSummary?.metrics.periodSessions,
                  }}
                />
                <KpiCard
                  icon={<RiPulseLine className="size-4" />}
                  label="Active Sessions"
                  accent="emerald"
                  to="/active-sessions"
                  value={formatNumber(summary.metrics.activeSessions)}
                  sub="live right now"
                />
                <KpiCard
                  icon={<RiServerLine className="size-4" />}
                  label="Trunks"
                  accent="sky"
                  to="/trunks"
                  value={formatNumber(summary.metrics.registeredTrunks)}
                  sub={
                    <>
                      Registered · {formatNumber(summary.metrics.enabledTrunks)}{' '}
                      / {formatNumber(summary.metrics.totalTrunks)} enabled
                    </>
                  }
                />
                <KpiCard
                  icon={<RiCheckboxCircleLine className="size-4" />}
                  label="Public Accounts"
                  accent="violet"
                  to="/public-accounts"
                  value={formatNumber(summary.metrics.publicAccounts)}
                />
                <KpiCard
                  icon={<RiRouteLine className="size-4" />}
                  label="Session Directory"
                  accent="amber"
                  to="/session-directory"
                  value={formatNumber(summary.metrics.sessionDirectoryNow)}
                  sub={
                    <>
                      WS Clients:{' '}
                      <Link
                        to="/ws-clients"
                        className="underline-offset-2 hover:underline"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {formatNumber(summary.metrics.wsClients)}
                      </Link>
                    </>
                  }
                />
              </div>
            ) : null}

            {/* KPI row 2 */}
            {showSkeletons ? (
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                {Array.from({ length: 4 }).map((_, i) => (
                  <KpiSkeleton key={i} />
                ))}
              </div>
            ) : summary ? (
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                <KpiCard
                  icon={<RiTimeLine className="size-4" />}
                  label="Avg Call Duration"
                  accent="slate"
                  value={formatDuration(summary.metrics.avgDurationSec)}
                  sub={`Max: ${formatDuration(summary.metrics.maxDurationSec)}`}
                  trend={{
                    current: summary.metrics.avgDurationSec,
                    previous: prevSummary?.metrics.avgDurationSec,
                    neutral: true,
                  }}
                />
                <KpiCard
                  icon={<RiArrowUpDownLine className="size-4" />}
                  label="Direction"
                  accent="sky"
                  value={`${formatNumber(inboundCount)} / ${formatNumber(outboundCount)}`}
                  sub={
                    <span className="inline-flex items-center gap-3">
                      <span className="inline-flex items-center gap-1.5">
                        <span className="size-2 rounded-full bg-sky-500" />
                        inbound
                      </span>
                      <span className="inline-flex items-center gap-1.5">
                        <span className="size-2 rounded-full bg-violet-500" />
                        outbound
                      </span>
                    </span>
                  }
                />
                <KpiCard
                  icon={<RiCheckboxCircleLine className="size-4" />}
                  label="Completed Rate"
                  accent="emerald"
                  hint="Share of completed sessions (ended or active) versus failed or unknown. In-progress states are excluded."
                  value={
                    successRate != null ? `${successRate.toFixed(1)}%` : 'N/A'
                  }
                  sub={
                    <>
                      Completed: {formatNumber(successFailure.success)} ·
                      Failed: {formatNumber(successFailure.failure)}
                    </>
                  }
                  progress={successRate ?? undefined}
                  trend={{
                    current: successRate ?? 0,
                    previous: prevSuccessRate ?? undefined,
                  }}
                />
                <Card className="border-l-2 border-l-slate-500/50 transition-all hover:-translate-y-0.5 hover:shadow-sm">
                  <CardContent className="p-4">
                    <div className="flex items-start justify-between gap-2">
                      <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                        vs Previous Period
                      </p>
                      <span className="text-slate-500 dark:text-slate-400">
                        <RiArrowUpDownLine className="size-4" />
                      </span>
                    </div>
                    {prevSummary ? (
                      <>
                        <p className="mt-2 text-2xl font-semibold tabular-nums leading-none">
                          {formatNumber(prevSummary.metrics.periodSessions)}
                        </p>
                        <p className="mt-1.5 text-xs text-muted-foreground">
                          previous period sessions
                        </p>
                        <div className="mt-2">
                          <TrendBadge
                            current={summary.metrics.periodSessions}
                            previous={prevSummary.metrics.periodSessions}
                          />
                        </div>
                      </>
                    ) : (
                      <p className="mt-2 text-sm text-muted-foreground">
                        No data
                      </p>
                    )}
                  </CardContent>
                </Card>
              </div>
            ) : null}
          </section>

          {/* ── Call Analytics Section ── */}
          <section className="mb-8 space-y-3">
            <SectionHeading
              icon={<RiBarChartGroupedLine className="size-4" />}
              title="Call Analytics"
              actionTo="/sessions"
              actionLabel="View sessions"
            />

            {showSkeletons ? (
              <div className="grid gap-4 xl:grid-cols-2">
                <ChartSkeleton />
                <ChartSkeleton />
                <ChartSkeleton />
              </div>
            ) : summary ? (
              <div className="grid gap-4 xl:grid-cols-2">
                {/* Sessions Over Time + Previous Period Comparison */}
                <Card>
                  <CardHeader className="pb-0">
                    <CardTitle className="flex items-center gap-2 text-sm">
                      <RiBarChartGroupedLine className="size-4" />
                      Sessions Over Time
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="pt-3">
                    {summary.series.length === 0 ? (
                      <EmptyState
                        icon={<RiInboxLine className="size-8" />}
                        message="No sessions in selected range"
                      />
                    ) : (
                      <div className="h-64 w-full">
                        <ResponsiveContainer width="100%" height="100%">
                          <LineChart data={comparisonSeries}>
                            <CartesianGrid
                              strokeDasharray="3 3"
                              strokeOpacity={0.25}
                            />
                            <XAxis dataKey="bucket" tick={{ fontSize: 11 }} />
                            <YAxis
                              allowDecimals={false}
                              tick={{ fontSize: 11 }}
                            />
                            <Tooltip content={<LineTooltip />} />
                            <Legend
                              formatter={(value: string) => (
                                <span className="text-xs text-muted-foreground">
                                  {value}
                                </span>
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
                <Card>
                  <CardHeader className="pb-0">
                    <CardTitle className="text-sm">
                      Session State Breakdown
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="pt-3">
                    {stateChartData.length === 0 ? (
                      <EmptyState
                        icon={<RiDatabaseLine className="size-8" />}
                        message="No state data in selected range"
                      />
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
                              label={({
                                name,
                                value,
                              }: {
                                name?: string
                                value?: number
                              }) => `${name} (${value})`}
                            >
                              {stateChartData.map((entry) => (
                                <Cell
                                  key={`${entry.state}-${entry.count}`}
                                  fill={
                                    STATE_COLORS[entry.state] ??
                                    STATE_COLORS.unknown
                                  }
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

                {/* Session outcome by state */}
                <Card className="xl:col-span-2">
                  <CardHeader className="pb-0">
                    <CardTitle className="text-sm">
                      Session Outcome by State
                    </CardTitle>
                    <p className="text-[11px] text-muted-foreground">
                      Green = completed · Red = failed · Amber = in progress
                    </p>
                  </CardHeader>
                  <CardContent className="pt-3">
                    {stateChartData.length === 0 ? (
                      <EmptyState
                        icon={<RiCheckboxCircleLine className="size-8" />}
                        message="No state data in selected range"
                      />
                    ) : (
                      <div className="h-64 w-full">
                        <ResponsiveContainer width="100%" height="100%">
                          <BarChart
                            data={stateChartData.map((s) => ({
                              name: s.state,
                              count: s.count,
                              fill: getStateOutcomeColor(s.state),
                            }))}
                            layout="vertical"
                          >
                            <CartesianGrid
                              strokeDasharray="3 3"
                              strokeOpacity={0.25}
                              horizontal={false}
                            />
                            <XAxis type="number" tick={{ fontSize: 11 }} />
                            <YAxis
                              type="category"
                              dataKey="name"
                              tick={{ fontSize: 11 }}
                              width={90}
                            />
                            <Tooltip content={<BarTooltip />} />
                            <Bar dataKey="count" radius={[0, 4, 4, 0]}>
                              {stateChartData.map((entry) => (
                                <Cell
                                  key={`sf-${entry.state}`}
                                  fill={getStateOutcomeColor(entry.state)}
                                />
                              ))}
                            </Bar>
                          </BarChart>
                        </ResponsiveContainer>
                      </div>
                    )}
                  </CardContent>
                </Card>
              </div>
            ) : null}
          </section>

          {/* ── SIP & Trunks Section ── */}
          <section className="space-y-3">
            <SectionHeading
              icon={<RiServerLine className="size-4" />}
              title="SIP & Trunks"
              actionTo="/trunks"
              actionLabel="Manage trunks"
            />

            {/* Terminal outcome KPIs */}
            {showSkeletons ? (
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-6">
                {Array.from({ length: 6 }).map((_, i) => (
                  <KpiSkeleton key={i} />
                ))}
              </div>
            ) : summary ? (
              terminalOutcomeSummary.length === 0 ? (
                <Card className="xl:col-span-6">
                  <CardContent className="p-4">
                    <EmptyState
                      icon={<RiServerLine className="size-8" />}
                      message="No terminal action data in selected range"
                    />
                  </CardContent>
                </Card>
              ) : (
                <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-6">
                  {terminalOutcomeSummary.slice(0, 6).map((item) => {
                    const accent: Accent =
                      item.key === 'accepted'
                        ? 'emerald'
                        : item.key === 'no_answer'
                          ? 'rose'
                          : item.key === 'busy' ||
                              item.key === 'caller_cancelled'
                            ? 'amber'
                            : item.key === 'outgoing_cancelled'
                              ? 'violet'
                              : 'slate'
                    return (
                      <Card
                        key={item.key}
                        className={cn(
                          'border-l-2 transition-all hover:-translate-y-0.5 hover:shadow-sm',
                          ACCENT_BORDER[accent],
                        )}
                      >
                        <CardContent className="p-4">
                          <div className="flex items-start justify-between gap-2">
                            <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
                              {item.label}
                            </p>
                            <span
                              className="size-2.5 shrink-0 rounded-full"
                              style={{ backgroundColor: item.color }}
                            />
                          </div>
                          <p className="mt-2 text-2xl font-semibold tabular-nums leading-none">
                            {formatNumber(item.count)}
                          </p>
                          <p className="mt-1.5 text-xs text-muted-foreground">
                            {item.sipStatusCode
                              ? `SIP ${item.sipStatusCode}`
                              : 'WS/SIP terminal action'}
                          </p>
                        </CardContent>
                      </Card>
                    )
                  })}
                </div>
              )
            ) : null}

            {/* SIP & Trunks charts */}
            {showSkeletons ? (
              <div className="grid gap-4 xl:grid-cols-2">
                <ChartSkeleton />
                <ChartSkeleton />
                <ChartSkeleton />
              </div>
            ) : summary ? (
              <div className="grid gap-4 xl:grid-cols-2">
                {/* SIP Terminal Outcomes by Direction */}
                <Card>
                  <CardHeader className="pb-0">
                    <CardTitle className="text-sm">
                      SIP Terminal Outcomes by Direction
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="pt-3">
                    {terminalOutcomeChartData.length === 0 ? (
                      <EmptyState
                        icon={<RiServerLine className="size-8" />}
                        message="No terminal outcome data in selected range"
                      />
                    ) : (
                      <div className="h-64 w-full">
                        <ResponsiveContainer width="100%" height="100%">
                          <BarChart data={terminalOutcomeChartData}>
                            <CartesianGrid
                              strokeDasharray="3 3"
                              strokeOpacity={0.25}
                            />
                            <XAxis
                              dataKey="outcomeLabel"
                              tick={{ fontSize: 10 }}
                            />
                            <YAxis
                              allowDecimals={false}
                              tick={{ fontSize: 11 }}
                            />
                            <Tooltip content={<BarTooltip />} />
                            <Bar
                              dataKey="count"
                              name="Count"
                              radius={[4, 4, 0, 0]}
                            >
                              {terminalOutcomeChartData.map((entry) => (
                                <Cell
                                  key={`${entry.outcome}-${entry.direction}-${entry.sipStatusCode}`}
                                  fill={entry.color}
                                />
                              ))}
                            </Bar>
                          </BarChart>
                        </ResponsiveContainer>
                      </div>
                    )}
                  </CardContent>
                </Card>

                {/* Top Trunks by SIP Terminal Reason */}
                <Card>
                  <CardHeader className="pb-0">
                    <CardTitle className="text-sm">
                      Top Trunks by Terminal Reason
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="pt-3">
                    {terminalTrunkRows.length === 0 ? (
                      <EmptyState
                        icon={<RiServerLine className="size-8" />}
                        message="No terminal trunk data in selected range"
                      />
                    ) : (
                      <div className="space-y-2">
                        {terminalTrunkRows.slice(0, 8).map((row) => (
                          <div
                            key={`${row.trunkKey}-${row.outcome}`}
                            className="flex items-center justify-between gap-3 rounded-md border border-border/60 px-3 py-2 text-xs"
                          >
                            <div className="min-w-0">
                              <p className="truncate font-medium">
                                {row.trunkName ||
                                  row.trunkKey ||
                                  'Unknown trunk'}
                              </p>
                              <p className="text-muted-foreground">
                                {row.outcomeLabel}
                              </p>
                            </div>
                            <span className="font-semibold tabular-nums">
                              {formatNumber(row.count)}
                            </span>
                          </div>
                        ))}
                      </div>
                    )}
                  </CardContent>
                </Card>

                {/* Top Trunks by Call Volume */}
                <Card className="xl:col-span-2">
                  <CardHeader className="pb-0">
                    <CardTitle className="text-sm">
                      Top Trunks by Call Volume
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="pt-3">
                    {trunkChartData.length === 0 ? (
                      <EmptyState
                        icon={<RiServerLine className="size-8" />}
                        message="No trunk volume data in selected range"
                      />
                    ) : (
                      <div className="h-64 w-full">
                        <ResponsiveContainer width="100%" height="100%">
                          <BarChart data={trunkChartData} layout="vertical">
                            <CartesianGrid
                              strokeDasharray="3 3"
                              strokeOpacity={0.25}
                              horizontal={false}
                            />
                            <XAxis type="number" tick={{ fontSize: 11 }} />
                            <YAxis
                              type="category"
                              dataKey="trunkName"
                              tick={{ fontSize: 11 }}
                              width={100}
                            />
                            <Tooltip content={<BarTooltip />} />
                            <Bar
                              dataKey="count"
                              fill={BAR_COLOR}
                              radius={[0, 4, 4, 0]}
                            />
                          </BarChart>
                        </ResponsiveContainer>
                      </div>
                    )}
                  </CardContent>
                </Card>
              </div>
            ) : null}
          </section>
            </>
          )}
        </div>
      </TooltipProvider>
    </div>
  )
}
