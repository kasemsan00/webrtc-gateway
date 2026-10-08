import { RiPhoneLine, RiPulseLine } from '@remixicon/react'
import { AnimatePresence, motion } from 'motion/react'
import type { ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import type { Trunk } from '@/features/trunk/types'

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

export function ActiveCallsDisplay({ count, className }: ActiveCallsDisplayProps) {
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

type TrunkOnCallCardShellProps = {
  trunk: Trunk
  children: ReactNode
}

export function TrunkOnCallCardShell({ trunk, children }: TrunkOnCallCardShellProps) {
  const onCall = isTrunkOnCall(trunk)
  const label = onCall
    ? `${trunk.name} — ${trunk.activeCallCount} active call${trunk.activeCallCount === 1 ? '' : 's'}`
    : undefined

  return (
    <div
      className="relative h-full rounded-xl"
      title={label}
      aria-label={label}
    >
      <AnimatePresence>
        {onCall ? (
          <motion.div
            key="trunk-on-call-ring"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className={cn(
              'pointer-events-none absolute -inset-[2px] z-0 overflow-hidden rounded-xl',
              'motion-reduce:inset-0 motion-reduce:ring-2 motion-reduce:ring-cyan-400/70',
            )}
            aria-hidden
          >
            <div
              className={cn(
                'absolute inset-[-50%] size-[200%]',
                'bg-[conic-gradient(from_0deg,#22d3ee,#34d399,#06b6d4,#6ee7b7,#22d3ee)]',
                'animate-trunk-ring-spin motion-reduce:hidden',
              )}
            />
          </motion.div>
        ) : null}
      </AnimatePresence>
      <div className="relative z-1 h-full">{children}</div>
    </div>
  )
}

export function ActiveDestinationDetailValue({
  destinations,
  onCall,
}: {
  destinations?: Array<string>
  onCall: boolean
}) {
  const label = formatActiveDestinationLabel(destinations) ?? '-'
  if (!onCall || label === '-') {
    return <span>{label}</span>
  }
  return (
    <span
      className="font-mono text-base font-bold tabular-nums text-cyan-950 dark:text-cyan-100"
      title={label}
    >
      {label}
    </span>
  )
}

export function TrunkOnCallHeaderBadge({ trunk }: { trunk: Trunk }) {
  if (!isTrunkOnCall(trunk)) return null
  return (
    <Badge variant="success" className="gap-1 text-[10px]">
      <span className="relative flex size-1.5 shrink-0">
        <span
          className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400 opacity-70 motion-reduce:animate-none"
          aria-hidden
        />
        <span
          className="relative inline-flex size-1.5 rounded-full bg-emerald-500"
          aria-hidden
        />
      </span>
      On call
    </Badge>
  )
}

export function PageActiveCallsChip({ totalCalls, trunksOnCall }: { totalCalls: number; trunksOnCall: number }) {
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
          <span className="relative inline-flex size-2 rounded-full bg-cyan-500" aria-hidden />
        </span>
      ) : (
        <RiPhoneLine className="size-3 opacity-60" aria-hidden />
      )}
      <span className="font-medium">{totalCalls}</span>
      <span className="text-[10px] opacity-80">calls</span>
    </div>
  )
}
