import {
  RiArrowLeftLine,
  RiArrowRightLine,
  RiCheckboxCircleFill,
  RiErrorWarningFill,
  RiForbidLine,
  RiPhoneFill,
  RiPhoneLine,
  RiServerLine,
  RiSmartphoneLine,
  RiUserVoiceFill,
  RiWifiOffLine,
} from '@remixicon/react'
import { AnimatePresence, motion } from 'motion/react'
import type { ComponentType, ReactNode } from 'react'

import type {
  LiveCall,
  LiveFilter,
  LiveSummary,
  TrunkLiveRow,
  TrunkLiveStatus,
} from '@/features/trunk/live-board'
import { formatCallDuration } from '@/features/trunk/live-board'
import { resolveWSClientPresenceLabel } from '@/features/ws-clients/types'
import { useNow } from '@/features/trunk/use-trunk-live-data'
import { cn } from '@/lib/utils'

type Tone =
  | 'talking'
  | 'reconnecting'
  | 'calling'
  | 'online'
  | 'error'
  | 'offline'

/** One palette for every node, link, pill and tile on the board. */
const TONE: Record<
  Tone,
  {
    text: string
    soft: string
    border: string
    accent: string
    dot: string
    line: string
  }
> = {
  talking: {
    text: 'text-emerald-700 dark:text-emerald-300',
    soft: 'bg-emerald-500/12 dark:bg-emerald-500/15',
    border: 'border-emerald-500/50',
    accent: 'border-l-emerald-500',
    dot: 'bg-emerald-500',
    line: 'text-emerald-500',
  },
  reconnecting: {
    text: 'text-orange-700 dark:text-orange-300',
    soft: 'bg-orange-500/12 dark:bg-orange-500/15',
    border: 'border-orange-500/50',
    accent: 'border-l-orange-500',
    dot: 'bg-orange-500',
    line: 'text-orange-500',
  },
  calling: {
    text: 'text-amber-700 dark:text-amber-300',
    soft: 'bg-amber-500/12 dark:bg-amber-500/15',
    border: 'border-amber-500/50',
    accent: 'border-l-amber-500',
    dot: 'bg-amber-500',
    line: 'text-amber-500',
  },
  online: {
    text: 'text-sky-700 dark:text-sky-300',
    soft: 'bg-sky-500/10 dark:bg-sky-500/15',
    border: 'border-sky-500/40',
    accent: 'border-l-sky-500',
    dot: 'bg-sky-500',
    line: 'text-sky-500/40',
  },
  error: {
    text: 'text-rose-700 dark:text-rose-300',
    soft: 'bg-rose-500/10 dark:bg-rose-500/15',
    border: 'border-rose-500/40',
    accent: 'border-l-rose-500',
    dot: 'bg-rose-500',
    line: 'text-rose-500/30',
  },
  offline: {
    text: 'text-zinc-600 dark:text-zinc-400',
    soft: 'bg-zinc-500/8 dark:bg-zinc-500/12',
    border: 'border-border',
    accent: 'border-l-zinc-300 dark:border-l-zinc-600',
    dot: 'bg-zinc-400 dark:bg-zinc-500',
    line: 'text-zinc-400/40',
  },
}

const STATUS_META: Record<
  TrunkLiveStatus,
  { tone: Tone; label: string; icon: ComponentType<{ className?: string }> }
> = {
  talking: { tone: 'talking', label: 'In call', icon: RiUserVoiceFill },
  reconnecting: {
    tone: 'reconnecting',
    label: 'Reconnecting',
    icon: RiUserVoiceFill,
  },
  dialing: { tone: 'calling', label: 'Calling', icon: RiPhoneFill },
  incoming: { tone: 'calling', label: 'Incoming call', icon: RiPhoneFill },
  busy: { tone: 'talking', label: 'In call', icon: RiUserVoiceFill },
  online: { tone: 'online', label: 'Online', icon: RiCheckboxCircleFill },
  error: { tone: 'error', label: 'Register failed', icon: RiErrorWarningFill },
  offline: { tone: 'offline', label: 'Offline', icon: RiWifiOffLine },
  disabled: { tone: 'offline', label: 'Disabled', icon: RiForbidLine },
}

function isLiveStatus(status: TrunkLiveStatus) {
  return (
    status === 'talking' ||
    status === 'reconnecting' ||
    status === 'dialing' ||
    status === 'incoming' ||
    status === 'busy'
  )
}

function callTone(call: LiveCall): Tone {
  if (call.phase === 'talking') return 'talking'
  if (call.phase === 'reconnecting') return 'reconnecting'
  return 'calling'
}

function callElapsed(call: LiveCall, now: number, fetchedAt: number) {
  return call.durationSec + Math.max(0, (now - fetchedAt) / 1000)
}

function callPhaseLabel(call: LiveCall) {
  switch (call.phase) {
    case 'talking':
      return 'Talking'
    case 'reconnecting':
      return 'Reconnecting'
    case 'incoming':
      return 'Incoming'
    case 'dialing':
    default:
      return 'Calling'
  }
}

function describeRow(row: TrunkLiveRow) {
  const meta = STATUS_META[row.status]
  const parties = row.calls.length
    ? row.calls
        .map((call) => `${callPhaseLabel(call)} ${call.remote.label}`)
        .join(', ')
    : row.fallbackDestinations.join(', ')
  return [row.trunk.name, meta.label, parties].filter(Boolean).join(' — ')
}

/* ------------------------------------------------------------------ */
/* Summary tiles                                                       */
/* ------------------------------------------------------------------ */

const TILES: Array<{
  filter: Exclude<LiveFilter, 'all'>
  tone: Tone
  label: string
  hint: string
  icon: ComponentType<{ className?: string }>
  value: (summary: LiveSummary) => number
  suffix?: (summary: LiveSummary) => string
}> = [
  {
    filter: 'online',
    tone: 'online',
    label: 'Online',
    hint: 'SIP registered',
    icon: RiCheckboxCircleFill,
    value: (summary) => summary.online,
    suffix: (summary) => `/ ${summary.trunks}`,
  },
  {
    filter: 'calling',
    tone: 'calling',
    label: 'Calling',
    hint: 'Dialing or ringing',
    icon: RiPhoneFill,
    value: (summary) => summary.calling,
  },
  {
    filter: 'talking',
    tone: 'talking',
    label: 'In call',
    hint: 'Connected calls',
    icon: RiUserVoiceFill,
    value: (summary) => summary.talking,
  },
  {
    filter: 'offline',
    tone: 'offline',
    label: 'Offline',
    hint: 'Not registered',
    icon: RiWifiOffLine,
    value: (summary) => summary.offline,
  },
]

function SummaryTiles({
  summary,
  filter,
  onFilterChange,
}: {
  summary: LiveSummary
  filter: LiveFilter
  onFilterChange: (filter: LiveFilter) => void
}) {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {TILES.map((tile) => {
        const tone = TONE[tile.tone]
        const value = tile.value(summary)
        const active = filter === tile.filter
        const Icon = tile.icon
        return (
          <button
            key={tile.filter}
            type="button"
            aria-pressed={active}
            onClick={() => onFilterChange(active ? 'all' : tile.filter)}
            className={cn(
              'group flex items-center gap-3 rounded-xl border bg-card px-4 py-3 text-left transition-all',
              'hover:shadow-sm focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
              active ? cn('border-2', tone.border, tone.soft) : 'border-border',
            )}
          >
            <span
              className={cn(
                'flex size-11 shrink-0 items-center justify-center rounded-full',
                tone.soft,
                tone.text,
              )}
            >
              <Icon className="size-6" />
            </span>
            <span className="min-w-0">
              <span className="flex items-baseline gap-1">
                <span
                  className={cn(
                    'text-3xl leading-none font-bold tabular-nums',
                    value > 0 ? tone.text : 'text-muted-foreground',
                  )}
                >
                  {value}
                </span>
                {tile.suffix ? (
                  <span className="text-sm text-muted-foreground tabular-nums">
                    {tile.suffix(summary)}
                  </span>
                ) : null}
              </span>
              <span className="mt-1 block text-sm font-semibold">
                {tile.label}
              </span>
              <span className="block text-xs text-muted-foreground">
                {active ? 'Showing only these · tap to clear' : tile.hint}
              </span>
            </span>
          </button>
        )
      })}
    </div>
  )
}

function Legend() {
  const items: Array<{ tone: Tone; label: string }> = [
    { tone: 'online', label: 'Online (ready)' },
    { tone: 'calling', label: 'Calling / ringing' },
    { tone: 'talking', label: 'In call' },
    { tone: 'reconnecting', label: 'Reconnecting' },
    { tone: 'error', label: 'Register failed' },
    { tone: 'offline', label: 'Offline / disabled' },
  ]
  return (
    <ul
      className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground"
      aria-label="Colour legend"
    >
      {items.map((item) => (
        <li key={item.tone} className="flex items-center gap-1.5">
          <span
            className={cn('size-2.5 rounded-full', TONE[item.tone].dot)}
            aria-hidden
          />
          {item.label}
        </li>
      ))}
    </ul>
  )
}

/* ------------------------------------------------------------------ */
/* Nodes                                                               */
/* ------------------------------------------------------------------ */

function StatusDot({ tone, live }: { tone: Tone; live: boolean }) {
  return (
    <span className="relative flex size-3 shrink-0" aria-hidden>
      {live ? (
        <span
          className={cn(
            'absolute inline-flex size-full animate-ping rounded-full opacity-60 motion-reduce:animate-none',
            TONE[tone].dot,
          )}
        />
      ) : null}
      <span
        className={cn(
          'relative inline-flex size-3 rounded-full ring-2 ring-background',
          TONE[tone].dot,
        )}
      />
    </span>
  )
}

function TrunkNode({
  row,
  actions,
}: {
  row: TrunkLiveRow
  actions?: ReactNode
}) {
  const { trunk, status, clients } = row
  const meta = STATUS_META[status]
  const tone = TONE[meta.tone]
  const Icon = meta.icon
  const agents = clients.filter(
    (client) => resolveWSClientPresenceLabel(client) === 'agent',
  ).length
  const apps = clients.length - agents
  const inUseBy = trunk.inUseBy ?? trunk.in_use_by

  return (
    <div className="flex min-w-0 items-start gap-3">
      <div
        className={cn(
          'relative flex size-12 shrink-0 items-center justify-center rounded-2xl border-2',
          tone.border,
          tone.soft,
          tone.text,
        )}
      >
        <RiServerLine className="size-6" aria-hidden />
        <span className="absolute -right-1 -bottom-1">
          <StatusDot tone={meta.tone} live={isLiveStatus(status)} />
        </span>
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <p
              className={cn(
                'truncate text-base leading-tight font-semibold',
                status === 'disabled' && 'text-muted-foreground line-through',
              )}
              title={trunk.name}
            >
              {trunk.name}
            </p>
            <p
              className="truncate font-mono text-xs text-muted-foreground"
              title={`${trunk.username}@${trunk.domain}:${trunk.port}`}
            >
              {trunk.username}@{trunk.domain}
            </p>
          </div>
          {actions}
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <span
            className={cn(
              'inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-sm font-semibold',
              tone.border,
              tone.soft,
              tone.text,
            )}
          >
            <Icon className="size-4" aria-hidden />
            {meta.label}
          </span>
          {clients.length > 0 ? (
            <span
              className="inline-flex items-center gap-1 rounded-full border border-violet-500/40 bg-violet-500/10 px-2 py-0.5 text-xs font-medium text-violet-700 dark:text-violet-300"
              title={clients
                .map(
                  (client) =>
                    `${resolveWSClientPresenceLabel(client)}${client.sipUsername ? ` · ${client.sipUsername}` : ''}`,
                )
                .join('\n')}
            >
              <RiSmartphoneLine className="size-3.5" aria-hidden />
              {[
                agents > 0 ? `${agents} agent${agents === 1 ? '' : 's'}` : '',
                apps > 0 ? `${apps} app${apps === 1 ? '' : 's'}` : '',
              ]
                .filter(Boolean)
                .join(' + ')}{' '}
              connected
            </span>
          ) : null}
          {trunk.isDefault ? (
            <span className="rounded-full border px-2 py-0.5 text-xs text-muted-foreground">
              Default
            </span>
          ) : null}
          {inUseBy ? (
            <span
              className="max-w-40 truncate rounded-full border px-2 py-0.5 text-xs text-muted-foreground"
              title={`In use by ${inUseBy}`}
            >
              by {inUseBy}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function RemoteNode({ call, tone }: { call: LiveCall; tone: Tone }) {
  const styles = TONE[tone]
  return (
    <div
      className={cn(
        'flex min-w-0 items-center gap-3 rounded-xl border-2 px-3 py-2',
        styles.border,
        styles.soft,
      )}
    >
      <span
        className={cn(
          'flex size-10 shrink-0 items-center justify-center rounded-full bg-background',
          styles.text,
        )}
      >
        {call.phase === 'talking' || call.phase === 'reconnecting' ? (
          <RiUserVoiceFill className="size-5" aria-hidden />
        ) : (
          <RiPhoneFill
            className="size-5 animate-live-phone-shake motion-reduce:animate-none"
            aria-hidden
          />
        )}
      </span>
      <div className="min-w-0">
        <p
          className="truncate font-mono text-lg leading-tight font-bold tabular-nums"
          title={call.remote.label}
        >
          {call.remote.label}
        </p>
        <p className="truncate text-xs text-muted-foreground">
          {call.direction === 'inbound'
            ? 'Caller'
            : call.direction === 'outbound'
              ? 'Destination'
              : 'Remote party'}
          {call.remote.detail ? ` · ${call.remote.detail}` : ''}
        </p>
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* Links                                                               */
/* ------------------------------------------------------------------ */

function LinkLine({
  tone,
  variant,
  reverse,
}: {
  tone: Tone
  variant: 'dashed-flow' | 'solid-flow' | 'dotted'
  reverse?: boolean
}) {
  const color = TONE[tone].line
  if (variant === 'dotted') {
    return (
      <span
        className={cn('live-link-dotted block h-0.5 w-full', color)}
        aria-hidden
      />
    )
  }
  if (variant === 'dashed-flow') {
    return (
      <span
        className={cn(
          'live-link-dashed animate-live-link-flow block h-1 w-full rounded-full motion-reduce:animate-none',
          reverse && '[animation-direction:reverse]',
          color,
        )}
        aria-hidden
      />
    )
  }
  return (
    <span className={cn('relative block h-1 w-full', color)} aria-hidden>
      <span className="absolute inset-0 rounded-full bg-current opacity-70" />
      <span className="animate-live-link-pulse absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-current shadow-[0_0_10px_currentColor] motion-reduce:hidden" />
      <span className="animate-live-link-pulse absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-current shadow-[0_0_10px_currentColor] [animation-direction:reverse] [animation-delay:-0.9s] motion-reduce:hidden" />
    </span>
  )
}

function CallLink({
  call,
  now,
  fetchedAt,
}: {
  call: LiveCall
  now: number
  fetchedAt: number
}) {
  const tone = callTone(call)
  const styles = TONE[tone]
  const ringing = call.phase === 'dialing' || call.phase === 'incoming'
  const elapsed = formatCallDuration(callElapsed(call, now, fetchedAt))
  const Arrow =
    call.direction === 'inbound' ? RiArrowLeftLine : RiArrowRightLine

  return (
    <div className="relative flex min-h-12 items-center">
      <LinkLine
        tone={tone}
        variant={
          ringing || call.phase === 'reconnecting'
            ? 'dashed-flow'
            : 'solid-flow'
        }
        reverse={call.direction === 'inbound'}
      />
      <span
        className={cn(
          'absolute left-1/2 -translate-x-1/2 inline-flex items-center gap-1.5 rounded-full border-2 bg-background px-3 py-1 text-sm font-semibold whitespace-nowrap shadow-sm',
          styles.border,
          styles.text,
        )}
      >
        {call.direction === 'inbound' ? (
          <Arrow className="size-4" aria-hidden />
        ) : null}
        {callPhaseLabel(call)}
        <span className="font-mono tabular-nums" aria-label={`for ${elapsed}`}>
          {elapsed}
        </span>
        {call.direction !== 'inbound' ? (
          <Arrow className="size-4" aria-hidden />
        ) : null}
      </span>
    </div>
  )
}

function FallbackCallLink({ destinations }: { destinations: Array<string> }) {
  const styles = TONE.talking
  return (
    <div className="grid items-center gap-3 sm:grid-cols-[minmax(6rem,1fr)_minmax(0,15rem)]">
      <div className="relative flex min-h-12 items-center">
        <LinkLine tone="talking" variant="solid-flow" />
        <span
          className={cn(
            'absolute left-1/2 -translate-x-1/2 rounded-full border-2 bg-background px-3 py-1 text-sm font-semibold whitespace-nowrap',
            styles.border,
            styles.text,
          )}
        >
          On call
        </span>
      </div>
      <div
        className={cn(
          'flex min-w-0 items-center gap-3 rounded-xl border-2 px-3 py-2',
          styles.border,
          styles.soft,
        )}
      >
        <RiPhoneLine
          className={cn('size-5 shrink-0', styles.text)}
          aria-hidden
        />
        <div className="min-w-0">
          <p className="truncate font-mono text-lg leading-tight font-bold">
            {destinations.length ? destinations.join(', ') : 'Unknown party'}
          </p>
          <p className="truncate text-xs text-muted-foreground">
            Call handled by another gateway instance
          </p>
        </div>
      </div>
    </div>
  )
}

function IdleLink({ row }: { row: TrunkLiveRow }) {
  const { status, trunk } = row
  const meta = STATUS_META[status]
  let text: ReactNode
  switch (status) {
    case 'online':
      text = 'Ready for calls'
      break
    case 'error':
      text = trunk.lastError || 'Registration failed'
      break
    case 'disabled':
      text = 'Trunk is disabled'
      break
    default:
      text = 'Not registered'
  }
  return (
    <div className="flex min-h-12 items-center gap-3">
      <LinkLine tone={meta.tone} variant="dotted" />
      <span
        className={cn(
          'max-w-[70%] shrink-0 truncate text-sm',
          status === 'error' ? TONE.error.text : 'text-muted-foreground',
        )}
        title={typeof text === 'string' ? text : undefined}
      >
        {text}
      </span>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* Lanes                                                               */
/* ------------------------------------------------------------------ */

function TrunkLane({
  row,
  now,
  fetchedAt,
  actions,
}: {
  row: TrunkLiveRow
  now: number
  fetchedAt: number
  actions?: ReactNode
}) {
  const meta = STATUS_META[row.status]
  const tone = TONE[meta.tone]
  const live = isLiveStatus(row.status)

  return (
    <motion.li
      layout
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.2 }}
      aria-label={describeRow(row)}
      className={cn(
        'grid gap-4 rounded-2xl border border-l-4 bg-card p-4 md:grid-cols-[minmax(15rem,20rem)_1fr] md:items-center',
        tone.accent,
        live && cn(tone.border, tone.soft),
        row.status === 'disabled' && 'opacity-60',
      )}
    >
      <TrunkNode row={row} actions={actions} />
      <div className="flex min-w-0 flex-col gap-2">
        {row.calls.length > 0 ? (
          row.calls.map((call) => (
            <div
              key={call.sessionId}
              className="grid items-center gap-3 sm:grid-cols-[minmax(6rem,1fr)_minmax(0,15rem)]"
            >
              <CallLink call={call} now={now} fetchedAt={fetchedAt} />
              <RemoteNode call={call} tone={callTone(call)} />
            </div>
          ))
        ) : row.status === 'busy' ? (
          <FallbackCallLink destinations={row.fallbackDestinations} />
        ) : (
          <IdleLink row={row} />
        )}
      </div>
    </motion.li>
  )
}

function OtherCallLane({
  call,
  now,
  fetchedAt,
}: {
  call: LiveCall
  now: number
  fetchedAt: number
}) {
  const tone = callTone(call)
  return (
    <li
      className={cn(
        'grid gap-4 rounded-2xl border border-l-4 bg-card p-4 md:grid-cols-[minmax(15rem,20rem)_1fr] md:items-center',
        TONE[tone].border,
        TONE[tone].accent,
      )}
      aria-label={`${call.local.label} — ${callPhaseLabel(call)} ${call.remote.label}`}
    >
      <div className="flex min-w-0 items-center gap-3">
        <span
          className={cn(
            'flex size-12 shrink-0 items-center justify-center rounded-2xl border-2',
            TONE[tone].border,
            TONE[tone].soft,
            TONE[tone].text,
          )}
        >
          <RiSmartphoneLine className="size-6" aria-hidden />
        </span>
        <div className="min-w-0">
          <p className="truncate text-base font-semibold">
            {call.sipUsername || call.local.label}
          </p>
          <p className="truncate text-xs text-muted-foreground">
            Direct (no trunk)
          </p>
        </div>
      </div>
      <div className="grid items-center gap-3 sm:grid-cols-[minmax(6rem,1fr)_minmax(0,15rem)]">
        <CallLink call={call} now={now} fetchedAt={fetchedAt} />
        <RemoteNode call={call} tone={tone} />
      </div>
    </li>
  )
}

/* ------------------------------------------------------------------ */
/* Board                                                               */
/* ------------------------------------------------------------------ */

export interface TrunkLiveBoardProps {
  rows: Array<TrunkLiveRow>
  otherCalls: Array<LiveCall>
  summary: LiveSummary
  filter: LiveFilter
  onFilterChange: (filter: LiveFilter) => void
  sessionsFetchedAt: number
  liveWarning?: string | null
  renderActions?: (row: TrunkLiveRow) => ReactNode
}

export function TrunkLiveBoard({
  rows,
  otherCalls,
  summary,
  filter,
  onFilterChange,
  sessionsFetchedAt,
  liveWarning,
  renderActions,
}: TrunkLiveBoardProps) {
  const visibleOtherCalls = otherCalls.filter((call) => {
    if (filter === 'all') return true
    if (filter === 'calling') {
      return call.phase === 'dialing' || call.phase === 'incoming'
    }
    if (filter === 'talking') {
      return call.phase === 'talking' || call.phase === 'reconnecting'
    }
    return false
  })
  const anyLive =
    visibleOtherCalls.length > 0 || rows.some((row) => row.calls.length > 0)
  const now = useNow(anyLive)

  return (
    <div className="flex flex-col gap-4">
      <SummaryTiles
        summary={summary}
        filter={filter}
        onFilterChange={onFilterChange}
      />
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Legend />
        {liveWarning ? (
          <span className="flex items-center gap-1 text-xs text-amber-700 dark:text-amber-300">
            <RiErrorWarningFill className="size-3.5" aria-hidden />
            {liveWarning}
          </span>
        ) : null}
      </div>

      {rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-2xl border border-dashed py-14 text-muted-foreground">
          <RiServerLine className="size-10 opacity-30" aria-hidden />
          <p className="text-sm">No trunks match this filter</p>
          {filter !== 'all' ? (
            <button
              type="button"
              className="text-sm font-medium text-primary underline-offset-4 hover:underline"
              onClick={() => onFilterChange('all')}
            >
              Show all trunks
            </button>
          ) : null}
        </div>
      ) : (
        <ul className="flex flex-col gap-3" aria-label="Trunks live status">
          <AnimatePresence initial={false}>
            {rows.map((row) => (
              <TrunkLane
                key={row.trunk.id}
                row={row}
                now={now}
                fetchedAt={sessionsFetchedAt}
                actions={renderActions?.(row)}
              />
            ))}
          </AnimatePresence>
        </ul>
      )}

      {visibleOtherCalls.length > 0 ? (
        <section
          className="flex flex-col gap-2"
          aria-label="Calls without a trunk"
        >
          <h2 className="text-sm font-semibold text-muted-foreground">
            Calls without a trunk
          </h2>
          <ul className="flex flex-col gap-3">
            {visibleOtherCalls.map((call) => (
              <OtherCallLane
                key={call.sessionId}
                call={call}
                now={now}
                fetchedAt={sessionsFetchedAt}
              />
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  )
}
