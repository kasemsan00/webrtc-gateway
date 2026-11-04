import { useStore } from '@tanstack/react-store'
import { Link } from '@tanstack/react-router'
import { RiRefreshLine } from '@remixicon/react'

import type { RealtimeSummaryState } from '@/lib/realtime-status-store'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  refreshGatewayStatus,
  useGatewayStatus,
} from '@/features/gateway-status/gateway-status-store'
import {
  OVERALL_LABEL,
  OVERALL_TONE,
  TONE_CLASSES,
  describeComponents,
  overallHealth,
} from '@/features/gateway-status/health'
import {
  realtimeStatusStore,
  summarizeRealtimeStreams,
} from '@/lib/realtime-status-store'
import { formatRelativeTime } from '@/lib/date-time'
import { useNow } from '@/lib/use-now'
import { cn } from '@/lib/utils'

function Dot({ className, pulse }: { className: string; pulse?: boolean }) {
  return (
    <span className="relative flex size-2 shrink-0">
      {pulse ? (
        <span
          className={cn(
            'absolute inline-flex size-full animate-ping rounded-full opacity-60',
            className,
          )}
        />
      ) : null}
      <span
        className={cn('relative inline-flex size-2 rounded-full', className)}
      />
    </span>
  )
}

const REALTIME_VIEW: Record<
  Exclude<RealtimeSummaryState, 'none'>,
  {
    label: string
    tone: 'success' | 'warning' | 'danger' | 'muted'
    hint: string
  }
> = {
  live: {
    label: 'Live',
    tone: 'success',
    hint: 'Realtime stream connected. The page updates as events arrive.',
  },
  connecting: {
    label: 'Connecting',
    tone: 'muted',
    hint: 'Opening the realtime stream…',
  },
  reconnecting: {
    label: 'Reconnecting',
    tone: 'danger',
    hint: 'Realtime stream dropped. Retrying with backoff; data may be stale until it reconnects (polling continues).',
  },
  paused: {
    label: 'Paused',
    tone: 'muted',
    hint: 'Tab is hidden, so the stream is paused. It resumes when you come back.',
  },
}

/** Shows whether this page's realtime (SSE) data is live. Hidden on pages without a stream. */
export function RealtimeIndicator() {
  const streams = useStore(realtimeStatusStore, (s) => s.streams)
  const list = Object.values(streams)
  const summary = summarizeRealtimeStreams(list)
  const now = useNow(5000)
  if (summary === 'none') return null
  const view = REALTIME_VIEW[summary]
  const tone = TONE_CLASSES[view.tone]
  const lastEvent = list.reduce<number | null>(
    (acc, s) =>
      s.lastEventAt && (!acc || s.lastEventAt > acc) ? s.lastEventAt : acc,
    null,
  )
  const title = [
    view.hint,
    ...list.map(
      (s) =>
        `${s.name}: ${s.state}${s.retryCount ? ` (retry ${s.retryCount})` : ''}`,
    ),
    lastEvent
      ? `Last event ${formatRelativeTime(lastEvent, now)}`
      : 'No events yet',
  ].join('\n')
  return (
    <span
      className={cn(
        'inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[11px] font-medium',
        tone.border,
        tone.text,
      )}
      title={title}
      role="status"
      aria-label={`Realtime: ${view.label}`}
    >
      <Dot className={tone.dot} pulse={summary === 'live'} />
      {view.label}
    </span>
  )
}

/** Compact gateway health pill; opens a panel with component states. */
export function GatewayHealthIndicator() {
  const { health, build, dashboard, error, updatedAt, loading } =
    useGatewayStatus()
  const now = useNow(5000)
  const overall =
    error && !health ? 'unknown' : overallHealth(health?.components)
  const toneKey = error && !health ? 'danger' : OVERALL_TONE[overall]
  const tone = TONE_CLASSES[toneKey]
  const components = describeComponents(health?.components)
  const label =
    error && !health ? 'Gateway unreachable' : OVERALL_LABEL[overall]

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className={cn(
            'inline-flex h-7 max-w-[220px] items-center gap-1.5 rounded-md border px-2 text-[11px] font-medium transition-colors hover:bg-muted',
            tone.border,
            tone.text,
          )}
          aria-label={`Gateway health: ${label}`}
        >
          <Dot className={tone.dot} pulse={overall === 'down'} />
          <span className="truncate">{label}</span>
          {build ? (
            <span className="hidden font-mono text-muted-foreground xl:inline">
              v{build.version}
            </span>
          ) : null}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[340px] p-0">
        <div className="space-y-1 border-b border-border px-3 py-2.5 text-xs">
          <div className="flex items-center justify-between gap-2">
            <p className="font-semibold">Gateway status</p>
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
              onClick={(event) => {
                event.preventDefault()
                void refreshGatewayStatus()
              }}
            >
              <RiRefreshLine
                className={cn('size-3', loading && 'animate-spin')}
              />
              Refresh
            </button>
          </div>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-[11px]">
            <dt className="text-muted-foreground">Instance</dt>
            <dd className="truncate font-mono" title={dashboard?.instanceId}>
              {dashboard?.instanceId ?? '-'}
            </dd>
            <dt className="text-muted-foreground">Version</dt>
            <dd className="truncate font-mono">
              {build
                ? `${build.version}${build.commit ? ` (${build.commit.slice(0, 7)})` : ''}${build.dirty ? ' dirty' : ''}`
                : '-'}
            </dd>
            {build?.buildTime ? (
              <>
                <dt className="text-muted-foreground">Built</dt>
                <dd className="font-mono">{build.buildTime}</dd>
              </>
            ) : null}
            <dt className="text-muted-foreground">Checked</dt>
            <dd>
              {updatedAt ? formatRelativeTime(updatedAt, now) : '-'} · every 15s
            </dd>
          </dl>
          {error ? (
            <p className="rounded border border-rose-500/30 bg-rose-500/10 px-2 py-1 text-[11px] text-rose-600 dark:text-rose-400">
              {error}
            </p>
          ) : null}
        </div>
        <ul className="max-h-[50vh] divide-y divide-border overflow-y-auto">
          {components.length === 0 ? (
            <li className="px-3 py-3 text-xs text-muted-foreground">
              No component health reported yet.
            </li>
          ) : (
            components.map((component) => {
              const c = TONE_CLASSES[component.tone]
              return (
                <li key={component.key} className="px-3 py-2 text-xs">
                  <div className="flex items-center gap-2">
                    <Dot className={c.dot} />
                    <span className="font-medium">{component.label}</span>
                    <span
                      className={cn('ml-auto text-[11px] font-medium', c.text)}
                    >
                      {component.stateLabel}
                    </span>
                  </div>
                  {component.reasonText ? (
                    <p className="mt-0.5 pl-4 text-[11px] text-muted-foreground">
                      {component.reasonText}
                    </p>
                  ) : null}
                  {component.detailText ? (
                    <p className="mt-0.5 pl-4 font-mono text-[10px] text-muted-foreground">
                      {component.detailText}
                    </p>
                  ) : null}
                </li>
              )
            })
          )}
        </ul>
        <div className="border-t border-border px-3 py-2 text-[11px]">
          <Link
            to="/"
            className="text-cyan-700 hover:underline dark:text-cyan-400"
          >
            Open dashboard health panel →
          </Link>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
