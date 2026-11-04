import type {
  OperationalHealthComponent,
  OperationalHealthState,
} from '@/features/dashboard/types'

export type OverallHealth = 'healthy' | 'degraded' | 'down' | 'unknown'

export interface ComponentHealthView {
  key: string
  label: string
  /** What this component does, for people new to the gateway. */
  purpose: string
  state: OperationalHealthState
  tone: 'success' | 'warning' | 'danger' | 'muted'
  stateLabel: string
  reason?: string
  reasonText?: string
  lastSuccessAt?: string
  details?: Record<string, number>
  /** Short facts derived from `details`, e.g. "depth 3/1000 · dropped 12". */
  detailText?: string
}

const COMPONENT_INFO: Record<string, { label: string; purpose: string }> = {
  database: {
    label: 'Database',
    purpose: 'Stores call history, SIP events, payloads and stats.',
  },
  eventQueue: {
    label: 'Event write queue',
    purpose: 'Buffers session events before they are written to the database.',
  },
  statsQueue: {
    label: 'Stats write queue',
    purpose:
      'Buffers RTCP/media stats before they are written to the database.',
  },
  sessionStore: {
    label: 'Session store',
    purpose: 'In-memory registry of live calls on this instance.',
  },
  sipListener: {
    label: 'SIP listener',
    purpose: 'UDP/TCP SIP stack that talks to trunks and the PBX.',
  },
  translator: {
    label: 'Translator',
    purpose: 'Live speech translation service used by translated calls.',
  },
  push: {
    label: 'Push notifications',
    purpose: 'FCM/APNs wake-up pushes for incoming calls to mobile apps.',
  },
  telemetry: {
    label: 'Telemetry export',
    purpose: 'Exports traces/metrics to the observability backend.',
  },
}

const REASON_TEXT: Record<string, string> = {
  database_logging_disabled: 'Database logging is turned off in config.',
  database_pool_unavailable: 'Cannot reach the database connection pool.',
  persistence_queue_dropping:
    'A write queue is full: events or stats are being dropped.',
  queue_dropping: 'Queue is full and dropping items. Check database latency.',
  queue_full: 'Queue is full.',
  readiness_not_reported:
    'Running, but this component does not report readiness.',
  translator_not_configured: 'Not configured.',
  push_not_configured: 'Not configured.',
  telemetry_disabled: 'Disabled in config.',
  store_not_configured: 'Not configured.',
  cached_probe_failed: 'The last background probe failed.',
  export_failed: 'Last export failed.',
  export_not_observed: 'No successful export observed yet.',
  dependency_unavailable: 'A dependency is unavailable.',
  startup: 'Starting up.',
  unavailable: 'Unavailable.',
}

const STATE_LABEL: Record<OperationalHealthState, string> = {
  connected: 'OK',
  degraded: 'Degraded',
  unavailable: 'Down',
  disabled: 'Disabled',
  unknown: 'Unknown',
}

export function humanizeKey(key: string): string {
  return key
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/[_-]+/g, ' ')
    .replace(/^./, (c) => c.toUpperCase())
}

export function reasonText(reason: string | undefined): string | undefined {
  if (!reason) return undefined
  return REASON_TEXT[reason] ?? humanizeKey(reason)
}

function toneFor(state: OperationalHealthState): ComponentHealthView['tone'] {
  switch (state) {
    case 'connected':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'unavailable':
      return 'danger'
    default:
      return 'muted'
  }
}

function detailTextFor(details: Record<string, number> | undefined) {
  if (!details) return undefined
  const parts: Array<string> = []
  if (
    typeof details.depth === 'number' &&
    typeof details.capacity === 'number'
  ) {
    parts.push(`depth ${details.depth}/${details.capacity}`)
  }
  for (const key of ['accepted', 'exported', 'dropped', 'failed']) {
    const value = details[key]
    if (typeof value === 'number') parts.push(`${key} ${value}`)
  }
  return parts.length ? parts.join(' · ') : undefined
}

const ORDER = [
  'database',
  'eventQueue',
  'statsQueue',
  'sessionStore',
  'sipListener',
  'translator',
  'push',
  'telemetry',
]

export function describeComponents(
  components: Record<string, OperationalHealthComponent> | undefined,
): Array<ComponentHealthView> {
  if (!components) return []
  const keys = Object.keys(components).sort((a, b) => {
    const ia = ORDER.indexOf(a)
    const ib = ORDER.indexOf(b)
    return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib) || a.localeCompare(b)
  })
  return keys.map((key) => {
    const component = components[key]
    const info = COMPONENT_INFO[key] as
      | { label: string; purpose: string }
      | undefined
    return {
      key,
      label: info?.label ?? humanizeKey(key),
      purpose: info?.purpose ?? '',
      state: component.state,
      tone: toneFor(component.state),
      stateLabel: STATE_LABEL[component.state],
      reason: component.reason,
      reasonText: reasonText(component.reason),
      lastSuccessAt: component.lastSuccessAt,
      details: component.details,
      detailText: detailTextFor(component.details),
    }
  })
}

/**
 * Overall verdict. Disabled and "unknown" (not reported) components do not
 * count against health: only explicit degraded / unavailable states do.
 */
export function overallHealth(
  components: Record<string, OperationalHealthComponent> | undefined,
): OverallHealth {
  if (!components) return 'unknown'
  const states = Object.values(components).map((c) => c.state)
  if (states.includes('unavailable')) return 'down'
  if (states.includes('degraded')) return 'degraded'
  return 'healthy'
}

export const OVERALL_LABEL: Record<OverallHealth, string> = {
  healthy: 'Healthy',
  degraded: 'Degraded',
  down: 'Component down',
  unknown: 'Health unknown',
}

export const OVERALL_TONE: Record<
  OverallHealth,
  'success' | 'warning' | 'danger' | 'muted'
> = {
  healthy: 'success',
  degraded: 'warning',
  down: 'danger',
  unknown: 'muted',
}

export const TONE_CLASSES: Record<
  'success' | 'warning' | 'danger' | 'muted',
  { dot: string; text: string; border: string; bg: string }
> = {
  success: {
    dot: 'bg-emerald-500',
    text: 'text-emerald-600 dark:text-emerald-400',
    border: 'border-emerald-500/30',
    bg: 'bg-emerald-500/10',
  },
  warning: {
    dot: 'bg-amber-500',
    text: 'text-amber-600 dark:text-amber-400',
    border: 'border-amber-500/30',
    bg: 'bg-amber-500/10',
  },
  danger: {
    dot: 'bg-rose-500',
    text: 'text-rose-600 dark:text-rose-400',
    border: 'border-rose-500/30',
    bg: 'bg-rose-500/10',
  },
  muted: {
    dot: 'bg-muted-foreground/60',
    text: 'text-muted-foreground',
    border: 'border-border',
    bg: 'bg-muted/40',
  },
}
