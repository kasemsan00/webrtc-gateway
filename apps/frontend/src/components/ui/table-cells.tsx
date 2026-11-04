import { RiCheckLine, RiFileCopyLine } from '@remixicon/react'
import { Link } from '@tanstack/react-router'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { formatRelativeTime, formatThaiDateTime } from '@/lib/date-time'
import { cn } from '@/lib/utils'

/**
 * Status badge for entities that have an `isExpired` boolean field.
 * Shows "Active" (success) or "Expired" (destructive).
 */
export function ExpiryStatusBadge({ isExpired }: { isExpired: boolean }) {
  return (
    <Badge
      variant={isExpired ? 'destructive' : 'success'}
      className="text-[10px]"
    >
      {isExpired ? 'Expired' : 'Active'}
    </Badge>
  )
}

/**
 * Timestamp cell. Shows the absolute Bangkok time; with `relative` it shows
 * "5m ago" and keeps the absolute time in the tooltip.
 */
export function TimestampCell({
  value,
  relative = false,
  now,
}: {
  value: string | null | undefined
  relative?: boolean
  now?: number
}) {
  if (!value) {
    return <span className="text-xs text-muted-foreground">-</span>
  }
  const absolute = formatThaiDateTime(value)
  return (
    <time
      dateTime={value}
      title={relative ? `${absolute} (${value})` : value}
      className="whitespace-nowrap text-xs text-muted-foreground"
    >
      {relative ? formatRelativeTime(value, now) : absolute}
    </time>
  )
}

/**
 * Renders text truncated to a max width in a monospaced muted span.
 */
export function MonoTruncateCell({
  value,
  maxWidth = 'max-w-[200px]',
}: {
  value: string
  maxWidth?: string
}) {
  return (
    <span
      className={`inline-block ${maxWidth} truncate font-mono text-xs text-muted-foreground`}
      title={value || undefined}
    >
      {value || '-'}
    </span>
  )
}

export async function copyText(text: string, label = 'Copied') {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(label, { description: text.length > 80 ? undefined : text })
    return true
  } catch {
    toast.error('Copy failed', {
      description: 'Clipboard access was blocked by the browser.',
    })
    return false
  }
}

/** Small icon button that copies `value`. Stops click propagation (row clicks). */
export function CopyButton({
  value,
  label = 'Copy',
  className,
}: {
  value: string
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    },
    [],
  )

  const handleClick = useCallback(
    async (event: React.MouseEvent) => {
      event.preventDefault()
      event.stopPropagation()
      if (!value) return
      if (await copyText(value, `${label} copied`)) {
        setCopied(true)
        if (timerRef.current) clearTimeout(timerRef.current)
        timerRef.current = setTimeout(() => setCopied(false), 1200)
      }
    },
    [label, value],
  )

  if (!value) return null
  return (
    <button
      type="button"
      onClick={handleClick}
      className={cn(
        'inline-flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-60 transition hover:bg-muted hover:text-foreground hover:opacity-100 focus-visible:opacity-100',
        className,
      )}
      aria-label={`${label}: ${value}`}
      title={`${label}: ${value}`}
    >
      {copied ? (
        <RiCheckLine className="size-3 text-emerald-500" />
      ) : (
        <RiFileCopyLine className="size-3" />
      )}
    </button>
  )
}

/**
 * Monospace identifier with a copy button. Long values are truncated with the
 * full value in the tooltip.
 */
export function IdCell({
  value,
  label = 'ID',
  maxWidth = 'max-w-[160px]',
  className,
}: {
  value: string | null | undefined
  label?: string
  maxWidth?: string
  className?: string
}) {
  if (!value) return <span className="text-xs text-muted-foreground">-</span>
  return (
    <span
      className={cn('inline-flex max-w-full items-center gap-0.5', className)}
    >
      <span
        className={cn('truncate font-mono text-[11px]', maxWidth)}
        title={value}
      >
        {value}
      </span>
      <CopyButton value={value} label={label} />
    </span>
  )
}

/** Session ID that links to the session detail page, with a copy button. */
export function SessionIdLink({
  sessionId,
  maxWidth = 'max-w-[160px]',
}: {
  sessionId: string | null | undefined
  maxWidth?: string
}) {
  if (!sessionId) {
    return <span className="text-xs text-muted-foreground">-</span>
  }
  return (
    <span className="inline-flex max-w-full items-center gap-0.5">
      <Link
        to="/sessions/$sessionId"
        params={{ sessionId }}
        onClick={(event) => event.stopPropagation()}
        className={cn(
          'truncate font-mono text-[11px] text-cyan-700 hover:underline dark:text-cyan-400',
          maxWidth,
        )}
        title={`Open session ${sessionId}`}
      >
        {sessionId}
      </Link>
      <CopyButton value={sessionId} label="Session ID" />
    </span>
  )
}
