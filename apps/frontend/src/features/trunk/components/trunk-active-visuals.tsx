import { RiPhoneLine, RiPulseLine } from '@remixicon/react'

import type { Trunk } from '@/features/trunk/types'
import { cn } from '@/lib/utils'

export function isTrunkOnCall(trunk: Pick<Trunk, 'activeCallCount'>) {
  return trunk.activeCallCount > 0
}

export function formatActiveDestinationLabel(
  destinations?: Array<string>,
): string | null {
  if (!destinations || destinations.length === 0) return null
  const label = destinations
    .map((destination) => destination.trim())
    .filter((destination) => destination.length > 0)
    .join(', ')
  return label.length > 0 ? label : null
}

export function trunkTableRowClassName(trunk: Trunk): string | undefined {
  if (!isTrunkOnCall(trunk)) return undefined
  return 'border-l-2 border-cyan-400/80 bg-cyan-500/5'
}

type ActiveCallsDisplayProps = {
  count: number
  className?: string
}

export function ActiveCallsDisplay({
  count,
  className,
}: ActiveCallsDisplayProps) {
  const live = count > 0
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 tabular-nums',
        live && 'font-semibold text-cyan-600 dark:text-cyan-400',
        className,
      )}
    >
      {live ? (
        <span className="relative flex size-2 shrink-0">
          <span
            className="absolute inline-flex size-full animate-ping rounded-full bg-cyan-400 opacity-60 motion-reduce:animate-none"
            aria-hidden
          />
          <span
            className="relative inline-flex size-2 rounded-full bg-cyan-500 motion-reduce:animate-none"
            aria-hidden
          />
        </span>
      ) : (
        <RiPhoneLine className="size-3 text-muted-foreground" aria-hidden />
      )}
      {count}
      {live ? (
        <RiPulseLine
          className="size-3 animate-pulse text-emerald-500 motion-reduce:animate-none"
          aria-hidden
        />
      ) : null}
    </span>
  )
}

export function PageActiveCallsChip({
  totalCalls,
  trunksOnCall,
}: {
  totalCalls: number
  trunksOnCall: number
}) {
  const live = totalCalls > 0
  return (
    <div
      className={cn(
        'flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs tabular-nums',
        live
          ? 'border-cyan-500/30 bg-cyan-500/10 text-cyan-700 dark:text-cyan-300'
          : 'border-border bg-muted/40 text-muted-foreground',
      )}
      title={
        live
          ? `${totalCalls} active call(s) across ${trunksOnCall} trunk(s) on this page`
          : 'No active calls on this page'
      }
    >
      {live ? (
        <span className="relative flex size-2 shrink-0">
          <span
            className="absolute inline-flex size-full animate-ping rounded-full bg-cyan-400 opacity-60 motion-reduce:animate-none"
            aria-hidden
          />
          <span
            className="relative inline-flex size-2 rounded-full bg-cyan-500"
            aria-hidden
          />
        </span>
      ) : (
        <RiPhoneLine className="size-3 opacity-60" aria-hidden />
      )}
      <span className="font-medium">{totalCalls}</span>
      <span className="text-[10px] opacity-80">calls</span>
    </div>
  )
}
