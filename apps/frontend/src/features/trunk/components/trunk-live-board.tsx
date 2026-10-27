import {
  RiArrowLeftLine,
  RiArrowLeftRightLine,
  RiArrowRightLine,
  RiCheckboxCircleFill,
  RiCustomerService2Line,
  RiErrorWarningFill,
  RiForbidLine,
  RiPhoneFill,
  RiPhoneLine,
  RiServerLine,
  RiSmartphoneLine,
  RiUserLine,
  RiUserVoiceFill,
  RiWifiOffLine,
} from '@remixicon/react'
import { AnimatePresence, motion } from 'motion/react'
import { useMemo, useState } from 'react'
import type { ComponentType, ReactNode } from 'react'

import type { ActiveSession } from '@/features/active-sessions/types'
import type { Trunk } from '@/features/trunk/types'
import type { WSClient } from '@/features/ws-clients/types'
import type {
  CallOperator,
  LiveCall,
  LiveFilter,
  LiveSummary,
  OperatorKind,
  TrunkLiveRow,
  TrunkLiveStatus,
} from '@/features/trunk/live-board'
import {
  buildTrunkLiveRows,
  callTimerSec,
  collectNonTrunkCalls,
  formatCallDuration,
  isActiveLiveRow,
  matchesLiveFilter,
  rowCallCount,
  summarizeLiveRows,
} from '@/features/trunk/live-board'
import { resolveWSClientPresenceLabel } from '@/features/ws-clients/types'
import { useNow } from '@/features/trunk/use-trunk-live-data'
import { cn } from '@/lib/utils'

type Tone =
  | 'talking'
  | 'reconnecting'
  | 'dialing'
  | 'incoming'
  | 'online'
  | 'error'
  | 'offline'

type IconType = ComponentType<{ className?: string }>

/**
 * Colour is reserved for status: the left accent border, the link line, the
 * status badge and the timer. Everything else stays neutral.
 */
const TONE: Record<
  Tone,
  {
    text: string
    badge: string
    pillBorder: string
    accent: string
    dot: string
    line: string
  }
> = {
  talking: {
    text: 'text-emerald-700 dark:text-emerald-300',
    badge: 'border-emerald-500/40 bg-emerald-500/10',
    pillBorder: 'border-emerald-500/50',
    accent: 'border-l-emerald-500',
    dot: 'bg-emerald-500',
    line: 'text-emerald-500',
  },
  reconnecting: {
    text: 'text-orange-700 dark:text-orange-300',
    badge: 'border-orange-500/40 bg-orange-500/10',
    pillBorder: 'border-orange-500/50',
    accent: 'border-l-orange-500',
    dot: 'bg-orange-500',
    line: 'text-orange-500',
  },
  dialing: {
    text: 'text-amber-700 dark:text-amber-300',
    badge: 'border-amber-500/40 bg-amber-500/10',
    pillBorder: 'border-amber-500/50',
    accent: 'border-l-amber-500',
    dot: 'bg-amber-500',
    line: 'text-amber-500',
  },
  incoming: {
    text: 'text-violet-700 dark:text-violet-300',
    badge: 'border-violet-500/40 bg-violet-500/10',
    pillBorder: 'border-violet-500/50',
    accent: 'border-l-violet-500',
    dot: 'bg-violet-500',
    line: 'text-violet-500',
  },
  online: {
    text: 'text-sky-700 dark:text-sky-300',
    badge: 'border-sky-500/40 bg-sky-500/10',
    pillBorder: 'border-sky-500/50',
    accent: 'border-l-sky-500',
    dot: 'bg-sky-500',
    line: 'text-sky-500',
  },
  error: {
    text: 'text-rose-700 dark:text-rose-300',
    badge: 'border-rose-500/40 bg-rose-500/10',
    pillBorder: 'border-rose-500/50',
    accent: 'border-l-rose-500',
    dot: 'bg-rose-500',
    line: 'text-rose-500',
  },
  offline: {
    text: 'text-zinc-600 dark:text-zinc-400',
    badge: 'border-border bg-muted/60',
    pillBorder: 'border-border',
    accent: 'border-l-zinc-300 dark:border-l-zinc-600',
    dot: 'bg-zinc-400 dark:bg-zinc-500',
    line: 'text-zinc-400',
  },
}

const STATUS_META: Record<
  TrunkLiveStatus,
  { tone: Tone; label: string; icon: IconType }
> = {
  talking: { tone: 'talking', label: 'In call', icon: RiUserVoiceFill },
  reconnecting: {
    tone: 'reconnecting',
    label: 'Reconnecting',
    icon: RiUserVoiceFill,
  },
  dialing: { tone: 'dialing', label: 'Dialing out', icon: RiPhoneFill },
  incoming: { tone: 'incoming', label: 'Incoming call', icon: RiPhoneFill },
  busy: { tone: 'talking', label: 'In call', icon: RiUserVoiceFill },
  online: { tone: 'online', label: 'Online', icon: RiCheckboxCircleFill },
  error: { tone: 'error', label: 'Register failed', icon: RiErrorWarningFill },
  offline: { tone: 'offline', label: 'Offline', icon: RiWifiOffLine },
  disabled: { tone: 'offline', label: 'Disabled', icon: RiForbidLine },
}

const OPERATOR_META: Record<OperatorKind, { label: string; icon: IconType }> = {
  agent: { label: 'Agent', icon: RiCustomerService2Line },
  app: { label: 'App', icon: RiSmartphoneLine },
  user: { label: 'User', icon: RiUserLine },
  line: { label: 'Line', icon: RiServerLine },
}

function callPhaseLabel(call: Pick<LiveCall, 'phase'>) {
  switch (call.phase) {
    case 'talking':
      return 'Talking'
    case 'reconnecting':
      return 'Reconnecting'
    case 'incoming':
      return 'Incoming'
    case 'dialing':
    default:
      return 'Dialing'
  }
}

function describeCall(call: LiveCall) {
  const who = call.operator
    ? `${OPERATOR_META[call.operator.kind].label} ${call.operator.label}`
    : 'Unknown'
  return `${who} ${callPhaseLabel(call).toLowerCase()} ${call.remote.label}`
}

/* ------------------------------------------------------------------ */
/* Summary tiles + legend                                              */
/* ------------------------------------------------------------------ */

const TILES: Array<{
  filter: Exclude<LiveFilter, 'all'>
  tone: Tone
  label: string
  icon: IconType
  value: (summary: LiveSummary) => number
  suffix?: (summary: LiveSummary) => string
  hint: (summary: LiveSummary) => string
}> = [
  {
    filter: 'online',
    tone: 'online',
    label: 'Online',
    icon: RiCheckboxCircleFill,
    value: (summary) => summary.online,
    suffix: (summary) => `/ ${summary.trunks}`,
    hint: () => 'SIP registered',
  },
  {
    filter: 'calling',
    tone: 'dialing',
    label: 'Calling',
    icon: RiPhoneFill,
    value: (summary) => summary.calling,
    hint: (summary) =>
      summary.calling > 0
        ? `${summary.dialing} out · ${summary.incoming} in`
        : 'Dialing or ringing',
  },
  {
    filter: 'talking',
    tone: 'talking',
    label: 'In call',
    icon: RiUserVoiceFill,
    value: (summary) => summary.talking,
    hint: (summary) =>
      summary.reconnecting > 0
        ? `${summary.talking - summary.reconnecting} talking · ${summary.reconnecting} reconnecting`
        : 'Connected calls',
  },
  {
    filter: 'offline',
    tone: 'offline',
    label: 'Offline',
    icon: RiWifiOffLine,
    value: (summary) => summary.offline,
    hint: () => 'Not registered or disabled',
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
              'flex items-center gap-3 rounded-xl border border-l-4 bg-card px-3 py-3 text-left transition-shadow sm:px-4',
              'hover:shadow-sm focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
              tone.accent,
              active && 'bg-muted/60 ring-2 ring-ring/40',
            )}
          >
            <Icon
              className={cn('hidden size-8 shrink-0 sm:block', tone.text)}
            />
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
                  <span className="text-base text-muted-foreground tabular-nums">
                    {tile.suffix(summary)}
                  </span>
                ) : null}
              </span>
              <span className="mt-1 block text-base font-semibold">
                {tile.label}
              </span>
              <span className="block text-sm text-muted-foreground">
                {active ? 'Filtered · tap to clear' : tile.hint(summary)}
              </span>
            </span>
          </button>
        )
      })}
    </div>
  )
}

const LEGEND: Array<{ tone: Tone; label: string }> = [
  { tone: 'online', label: 'Online' },
  { tone: 'dialing', label: 'Dialing out' },
  { tone: 'incoming', label: 'Incoming' },
  { tone: 'talking', label: 'In call' },
  { tone: 'reconnecting', label: 'Reconnecting' },
  { tone: 'error', label: 'Register failed' },
  { tone: 'offline', label: 'Offline / disabled' },
]

function Legend() {
  return (
    <ul
      className="-mx-1 flex max-w-full min-w-0 flex-nowrap items-center gap-x-4 overflow-x-auto px-1 pb-1 text-xs whitespace-nowrap text-muted-foreground sm:flex-wrap sm:gap-y-1 sm:overflow-visible sm:pb-0"
      aria-label="Colour legend"
    >
      {LEGEND.map((item) => (
        <li key={item.tone} className="flex shrink-0 items-center gap-1.5">
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
/* Shared bits                                                         */
/* ------------------------------------------------------------------ */

function StatusDot({ tone, live }: { tone: Tone; live: boolean }) {
  return (
    <span className="relative flex size-2.5 shrink-0" aria-hidden>
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
          'relative inline-flex size-2.5 rounded-full',
          TONE[tone].dot,
        )}
      />
    </span>
  )
}

function StatusBadge({ status }: { status: TrunkLiveStatus }) {
  const meta = STATUS_META[status]
  const tone = TONE[meta.tone]
  const Icon = meta.icon
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-semibold whitespace-nowrap',
        tone.badge,
        tone.text,
      )}
    >
      <Icon className="size-3.5" aria-hidden />
      {meta.label}
    </span>
  )
}

function ConnectedChip({ row }: { row: TrunkLiveRow }) {
  const { clients } = row
  if (clients.length === 0) return null
  const agents = clients.filter(
    (client) => resolveWSClientPresenceLabel(client) === 'agent',
  ).length
  const apps = clients.length - agents
  const label = [
    agents > 0 ? `${agents} agent${agents === 1 ? '' : 's'}` : '',
    apps > 0 ? `${apps} app${apps === 1 ? '' : 's'}` : '',
  ]
    .filter(Boolean)
    .join(' + ')
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs whitespace-nowrap text-muted-foreground"
      title={clients
        .map(
          (client) =>
            `${resolveWSClientPresenceLabel(client)}${client.sipUsername ? ` · ${client.sipUsername}` : ''}`,
        )
        .join('\n')}
    >
      <RiSmartphoneLine className="size-3.5" aria-hidden />
      {label} online
    </span>
  )
}

/* ------------------------------------------------------------------ */
/* Active call rows                                                    */
/* ------------------------------------------------------------------ */

function OperatorNode({ operator }: { operator: CallOperator | null }) {
  const meta = OPERATOR_META[operator?.kind ?? 'line']
  const Icon = meta.icon
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <span className="hidden size-9 shrink-0 items-center justify-center rounded-full border bg-muted/50 text-muted-foreground sm:flex">
        <Icon className="size-4.5" aria-hidden />
      </span>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{meta.label}</p>
        <p
          className="truncate text-base leading-tight font-bold sm:text-lg"
          title={operator?.label}
        >
          {operator?.label ?? 'Unknown'}
        </p>
      </div>
    </div>
  )
}

function RemoteNode({
  label,
  caption,
  ringing,
}: {
  label: string
  caption: string
  ringing?: boolean
}) {
  return (
    <div className="flex min-w-0 items-center justify-end gap-2.5 text-right md:justify-start md:text-left">
      <span className="hidden size-9 shrink-0 items-center justify-center rounded-full border bg-muted/50 text-muted-foreground sm:flex">
        <RiPhoneLine
          className={cn(
            'size-4.5',
            ringing && 'animate-live-phone-shake motion-reduce:animate-none',
          )}
          aria-hidden
        />
      </span>
      <div className="min-w-0">
        <p className="truncate text-xs text-muted-foreground" title={caption}>
          {caption}
        </p>
        <p
          className="truncate font-mono text-base leading-tight font-bold tabular-nums sm:text-lg"
          title={label}
        >
          {label}
        </p>
      </div>
    </div>
  )
}

function LinkLine({
  tone,
  flowing,
  reverse,
}: {
  tone: Tone
  flowing: 'dashed' | 'solid' | 'static'
  reverse?: boolean
}) {
  const color = TONE[tone].line
  if (flowing === 'dashed') {
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
  // Connected: a calm base line with one streak moving the call's way
  // (outbound left→right, inbound right→left). `static` = direction unknown.
  return (
    <span
      className={cn(
        'relative block h-1 w-full overflow-hidden rounded-full',
        color,
      )}
      aria-hidden
    >
      <span className="absolute inset-0 rounded-full bg-current opacity-35" />
      {flowing === 'solid' ? (
        <span
          className={cn(
            'animate-live-link-stream absolute inset-y-0 w-2/5 rounded-full from-transparent to-current motion-reduce:hidden',
            reverse
              ? 'bg-linear-to-l [animation-direction:reverse]'
              : 'bg-linear-to-r',
          )}
        />
      ) : null}
    </span>
  )
}

function LinkCell({
  tone,
  flowing,
  reverse,
  children,
}: {
  tone: Tone
  flowing: 'dashed' | 'solid' | 'static'
  reverse?: boolean
  children: ReactNode
}) {
  return (
    <div className="relative order-last col-span-3 flex min-h-9 items-center md:order-none md:col-span-1">
      <LinkLine tone={tone} flowing={flowing} reverse={reverse} />
      <span
        className={cn(
          'absolute left-1/2 inline-flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-background px-3 py-0.5 text-sm font-semibold whitespace-nowrap shadow-xs',
          TONE[tone].pillBorder,
          TONE[tone].text,
        )}
      >
        {children}
      </span>
    </div>
  )
}

/** operator · link · remote on desktop; operator ↔ remote, then link, on mobile. */
const CALL_GRID =
  'grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-x-3 gap-y-2 md:grid-cols-[minmax(0,14rem)_minmax(9rem,1fr)_minmax(0,15rem)]'

function CallFlow({
  call,
  now,
  fetchedAt,
}: {
  call: LiveCall
  now: number
  fetchedAt: number
}) {
  const tone: Tone = call.phase
  const ringing = call.phase === 'dialing' || call.phase === 'incoming'
  const inbound = call.direction === 'inbound'
  const elapsed = formatCallDuration(callTimerSec(call, now, fetchedAt))
  const Arrow = inbound ? RiArrowLeftLine : RiArrowRightLine
  const MobileGlyph = ringing ? Arrow : RiArrowLeftRightLine

  return (
    <div className={CALL_GRID} aria-label={describeCall(call)}>
      <OperatorNode operator={call.operator} />
      <LinkCell
        tone={tone}
        flowing={ringing || call.phase === 'reconnecting' ? 'dashed' : 'solid'}
        reverse={inbound}
      >
        {inbound ? <Arrow className="size-4" aria-hidden /> : null}
        {callPhaseLabel(call)}
        <span className="font-mono tabular-nums">{elapsed}</span>
        {!inbound ? <Arrow className="size-4" aria-hidden /> : null}
      </LinkCell>
      <MobileGlyph
        className={cn('size-5 md:hidden', TONE[tone].text)}
        aria-hidden
      />
      <RemoteNode
        label={call.remote.label}
        caption={[inbound ? 'Caller' : 'Destination', call.remote.detail]
          .filter(Boolean)
          .join(' · ')}
        ringing={ringing}
      />
    </div>
  )
}

function FallbackCallFlow({ row }: { row: TrunkLiveRow }) {
  const inUseBy = (row.trunk.inUseBy ?? row.trunk.in_use_by ?? '').trim()
  const operator: CallOperator = inUseBy
    ? { kind: 'user', label: inUseBy }
    : { kind: 'line', label: row.trunk.username }
  return (
    <div className={CALL_GRID}>
      <OperatorNode operator={operator} />
      <LinkCell tone="talking" flowing="static">
        On call
      </LinkCell>
      <RiArrowLeftRightLine
        className={cn('size-5 md:hidden', TONE.talking.text)}
        aria-hidden
      />
      <RemoteNode
        label={
          row.fallbackDestinations.length
            ? row.fallbackDestinations.join(', ')
            : 'Unknown'
        }
        caption="On another gateway instance"
      />
    </div>
  )
}

const LANE_GRID =
  'grid gap-3 rounded-xl border border-l-4 bg-card p-3 sm:p-4 md:grid-cols-[minmax(11rem,15rem)_1fr] md:items-center md:gap-5'

function ActiveLane({
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
  return (
    <motion.li
      layout
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.2 }}
      className={cn(LANE_GRID, TONE[meta.tone].accent)}
    >
      <div className="flex min-w-0 items-start justify-between gap-2 md:flex-col md:items-stretch">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <StatusDot tone={meta.tone} live />
            <p
              className="truncate text-sm font-semibold"
              title={row.trunk.name}
            >
              {row.trunk.name}
            </p>
          </div>
          <p
            className="truncate pl-4.5 font-mono text-xs text-muted-foreground"
            title={`${row.trunk.username}@${row.trunk.domain}:${row.trunk.port}`}
          >
            {row.trunk.username}@{row.trunk.domain}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1 md:justify-between md:pl-4.5">
          <StatusBadge status={row.status} />
          {actions}
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-3 md:border-l md:pl-5">
        {row.calls.length > 0 ? (
          row.calls.map((call) => (
            <CallFlow
              key={call.sessionId}
              call={call}
              now={now}
              fetchedAt={fetchedAt}
            />
          ))
        ) : (
          <FallbackCallFlow row={row} />
        )}
      </div>
    </motion.li>
  )
}

function DirectCallLane({
  call,
  now,
  fetchedAt,
}: {
  call: LiveCall
  now: number
  fetchedAt: number
}) {
  return (
    <li className={cn(LANE_GRID, TONE[call.phase].accent)}>
      <div className="flex min-w-0 items-center gap-2">
        <StatusDot tone={call.phase} live />
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">Direct call</p>
          <p className="truncate text-xs text-muted-foreground">No trunk</p>
        </div>
      </div>
      <div className="min-w-0 md:border-l md:pl-5">
        <CallFlow call={call} now={now} fetchedAt={fetchedAt} />
      </div>
    </li>
  )
}

/* ------------------------------------------------------------------ */
/* Compact cards for idle / offline trunks                             */
/* ------------------------------------------------------------------ */

function ErrorLine({ message }: { message: string }) {
  const [expanded, setExpanded] = useState(false)
  return (
    <button
      type="button"
      onClick={() => setExpanded((value) => !value)}
      aria-expanded={expanded}
      aria-label={expanded ? 'Collapse error' : `Show full error: ${message}`}
      title={expanded ? undefined : message}
      className={cn(
        'w-full rounded-md bg-rose-500/8 px-2 py-1 text-left text-xs text-rose-700 hover:bg-rose-500/12 dark:text-rose-300',
        expanded ? 'break-words whitespace-normal' : 'truncate',
      )}
    >
      {message}
    </button>
  )
}

function CompactTrunkCard({
  row,
  actions,
}: {
  row: TrunkLiveRow
  actions?: ReactNode
}) {
  const { trunk, status } = row
  const meta = STATUS_META[status]
  const tone = TONE[meta.tone]
  return (
    <motion.li
      layout
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.2 }}
      className={cn(
        'flex min-w-0 flex-col gap-1.5 rounded-xl border border-l-4 bg-card px-3 py-2.5',
        tone.accent,
        status === 'disabled' && 'opacity-60',
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <StatusDot tone={meta.tone} live={false} />
        <p
          className={cn(
            'min-w-0 flex-1 truncate text-sm font-semibold',
            status === 'disabled' && 'line-through',
          )}
          title={trunk.name}
        >
          {trunk.name}
        </p>
        {actions}
      </div>
      <p
        className="truncate pl-4.5 font-mono text-xs text-muted-foreground"
        title={`${trunk.username}@${trunk.domain}:${trunk.port}`}
      >
        {trunk.username}@{trunk.domain}
      </p>
      <div className="flex flex-wrap items-center gap-1.5 pl-4.5">
        <span className={cn('text-xs font-semibold', tone.text)}>
          {meta.label}
        </span>
        <ConnectedChip row={row} />
        {trunk.isDefault ? (
          <span className="rounded-full border px-1.5 text-[11px] text-muted-foreground">
            Default
          </span>
        ) : null}
      </div>
      {status === 'error' && trunk.lastError ? (
        <ErrorLine message={trunk.lastError} />
      ) : null}
    </motion.li>
  )
}

/* ------------------------------------------------------------------ */
/* Board                                                               */
/* ------------------------------------------------------------------ */

export interface TrunkLiveBoardProps {
  /** Raw or normalised API data; mapping happens inside the board. */
  trunks: Array<Trunk>
  sessions: Array<ActiveSession>
  clients: Array<WSClient>
  filter: LiveFilter
  onFilterChange: (filter: LiveFilter) => void
  sessionsFetchedAt: number
  liveWarning?: string | null
  renderActions?: (row: TrunkLiveRow) => ReactNode
}

/**
 * Builds rows from API data inside the board so mapping errors are caught by
 * the surrounding LiveBoardErrorBoundary instead of blanking the page.
 */
export function TrunkLiveBoard({
  trunks,
  sessions,
  clients,
  filter,
  onFilterChange,
  sessionsFetchedAt,
  liveWarning,
  renderActions,
}: TrunkLiveBoardProps) {
  const allRows = useMemo(
    () => buildTrunkLiveRows(trunks, sessions, clients),
    [trunks, sessions, clients],
  )
  const otherCalls = useMemo(
    () => collectNonTrunkCalls(sessions, clients),
    [sessions, clients],
  )
  const summary = useMemo(
    () => summarizeLiveRows(allRows, otherCalls),
    [allRows, otherCalls],
  )
  const rows = useMemo(
    () => allRows.filter((row) => matchesLiveFilter(row, filter)),
    [allRows, filter],
  )
  const activeRows = rows.filter(isActiveLiveRow)
  const idleRows = rows.filter((row) => !isActiveLiveRow(row))
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
  const liveCount =
    activeRows.reduce((count, row) => count + rowCallCount(row), 0) +
    visibleOtherCalls.length
  const showLiveSection =
    filter === 'all' || filter === 'calling' || filter === 'talking'
  const now = useNow(
    visibleOtherCalls.length > 0 ||
      activeRows.some((row) => row.calls.length > 0),
  )

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <SummaryTiles
        summary={summary}
        filter={filter}
        onFilterChange={onFilterChange}
      />
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <Legend />
        {liveWarning ? (
          <span className="flex items-center gap-1 text-xs text-amber-700 dark:text-amber-300">
            <RiErrorWarningFill className="size-3.5" aria-hidden />
            {liveWarning}
          </span>
        ) : null}
      </div>

      {showLiveSection ? (
        <section className="flex flex-col gap-2" aria-label="Live calls">
          <h2 className="text-sm font-semibold text-muted-foreground">
            Live calls <span className="tabular-nums">({liveCount})</span>
          </h2>
          {liveCount === 0 ? (
            <p className="rounded-xl border border-dashed px-4 py-4 text-sm text-muted-foreground">
              No calls right now.
            </p>
          ) : (
            <ul className="flex flex-col gap-2.5">
              <AnimatePresence initial={false}>
                {activeRows.map((row) => (
                  <ActiveLane
                    key={row.trunk.id}
                    row={row}
                    now={now}
                    fetchedAt={sessionsFetchedAt}
                    actions={renderActions?.(row)}
                  />
                ))}
              </AnimatePresence>
              {visibleOtherCalls.map((call) => (
                <DirectCallLane
                  key={call.sessionId}
                  call={call}
                  now={now}
                  fetchedAt={sessionsFetchedAt}
                />
              ))}
            </ul>
          )}
        </section>
      ) : null}

      {idleRows.length > 0 ? (
        <section
          className="flex flex-col gap-2"
          aria-label="Trunks not in a call"
        >
          <h2 className="text-sm font-semibold text-muted-foreground">
            Not in a call{' '}
            <span className="tabular-nums">({idleRows.length})</span>
          </h2>
          <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
            <AnimatePresence initial={false}>
              {idleRows.map((row) => (
                <CompactTrunkCard
                  key={row.trunk.id}
                  row={row}
                  actions={renderActions?.(row)}
                />
              ))}
            </AnimatePresence>
          </ul>
        </section>
      ) : null}

      {rows.length === 0 && !showLiveSection ? (
        <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed py-12 text-muted-foreground">
          <RiServerLine className="size-10 opacity-30" aria-hidden />
          <p className="text-sm">No trunks match this filter</p>
          <button
            type="button"
            className="text-sm font-medium text-primary underline-offset-4 hover:underline"
            onClick={() => onFilterChange('all')}
          >
            Show all trunks
          </button>
        </div>
      ) : null}
    </div>
  )
}
