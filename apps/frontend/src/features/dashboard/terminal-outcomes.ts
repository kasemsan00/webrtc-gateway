import type {
  DashboardSummaryTerminalOutcomePoint,
  DashboardSummaryTerminalTrunkPoint,
} from './types'

export const TERMINAL_OUTCOME_LABELS: Record<string, string> = {
  accepted: 'Accepted',
  busy: 'Declined / Busy',
  decline: 'Declined / Busy',
  no_answer: 'No Answer / Offline',
  caller_cancelled: 'Caller Cancelled',
  outgoing_cancelled: 'Outgoing Cancelled',
  hangup: 'BYE / Hangup',
  bye: 'BYE / Hangup',
}

export const TERMINAL_OUTCOME_COLORS: Record<string, string> = {
  accepted: '#22c55e',
  busy: '#f59e0b',
  decline: '#f59e0b',
  no_answer: '#ef4444',
  caller_cancelled: '#64748b',
  outgoing_cancelled: '#a855f7',
  hangup: '#06b6d4',
  bye: '#06b6d4',
  other: '#9ca3af',
}

export interface TerminalOutcomeSummary {
  key: string
  label: string
  count: number
  sipStatusCode?: number
  color: string
}

function normalizeOutcome(outcome: string) {
  const normalized = outcome.trim().toLowerCase()
  if (normalized === 'declined' || normalized === 'decline') return 'busy'
  if (normalized === 'timeout' || normalized === 'offline') return 'no_answer'
  if (normalized === 'bye') return 'hangup'
  return normalized || 'other'
}

export function formatTerminalOutcomeLabel(outcome: string) {
  const key = normalizeOutcome(outcome)
  return TERMINAL_OUTCOME_LABELS[key] ?? (outcome || 'Other')
}

export function summarizeTerminalOutcomes(
  points: Array<DashboardSummaryTerminalOutcomePoint>,
): Array<TerminalOutcomeSummary> {
  const byOutcome = new Map<string, TerminalOutcomeSummary>()
  for (const point of points) {
    const key = normalizeOutcome(point.outcome)
    const existing = byOutcome.get(key)
    if (existing) {
      existing.count += point.count
      if (!existing.sipStatusCode && point.sipStatusCode > 0) {
        existing.sipStatusCode = point.sipStatusCode
      }
      continue
    }
    byOutcome.set(key, {
      key,
      label: formatTerminalOutcomeLabel(key),
      count: point.count,
      sipStatusCode: point.sipStatusCode > 0 ? point.sipStatusCode : undefined,
      color: TERMINAL_OUTCOME_COLORS[key] ?? TERMINAL_OUTCOME_COLORS.other,
    })
  }
  return Array.from(byOutcome.values()).sort((a, b) => b.count - a.count)
}

export function buildTerminalOutcomeChartData(
  points: Array<DashboardSummaryTerminalOutcomePoint>,
) {
  return points.map((point) => ({
    ...point,
    outcomeLabel: formatTerminalOutcomeLabel(point.outcome),
    directionLabel: point.direction || 'unknown',
    color:
      TERMINAL_OUTCOME_COLORS[normalizeOutcome(point.outcome)] ??
      TERMINAL_OUTCOME_COLORS.other,
  }))
}

export function buildTerminalTrunkRows(
  points: Array<DashboardSummaryTerminalTrunkPoint>,
) {
  return points.map((point) => ({
    ...point,
    outcomeLabel: formatTerminalOutcomeLabel(point.outcome),
  }))
}
