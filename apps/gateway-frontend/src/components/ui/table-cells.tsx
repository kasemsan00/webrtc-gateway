import { Badge } from '@/components/ui/badge'
import { formatThaiDateTime } from '@/lib/date-time'

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
 * Renders an ISO timestamp formatted for Thai locale in a monospaced muted span.
 */
export function TimestampCell({ value }: { value: string }) {
  return (
    <span className="text-xs text-muted-foreground">
      {formatThaiDateTime(value)}
    </span>
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
    >
      {value || '-'}
    </span>
  )
}
