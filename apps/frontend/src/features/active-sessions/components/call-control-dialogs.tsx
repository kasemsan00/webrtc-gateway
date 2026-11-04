import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  hangupSession,
  sendSessionDtmf,
} from '@/features/active-sessions/services/session-control-api'

export const DTMF_PATTERN = /^[0-9*#]+$/
export const DTMF_MAX_LEN = 32

export function validateDtmf(digits: string): string | null {
  const value = digits.trim()
  if (!value || !DTMF_PATTERN.test(value) || value.length > DTMF_MAX_LEN) {
    return 'DTMF must be 1–32 characters: digits 0-9, * or # only'
  }
  return null
}

export interface CallTarget {
  sessionId: string
  /** Human context shown in the dialog, e.g. "alice → 1001". */
  label?: string
}

/** Confirms and sends SIP BYE for a live session. */
export function HangupDialog({
  target,
  onClose,
  onDone,
}: {
  target: CallTarget | null
  onClose: () => void
  onDone?: () => void
}) {
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (target) setError(null)
  }, [target])

  const submit = async () => {
    if (!target) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await hangupSession(target.sessionId)
      toast.success('Hangup sent', {
        description: res.message || `Session ${target.sessionId}`,
      })
      onClose()
      onDone?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to hang up session')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Hang up this call?</DialogTitle>
          <DialogDescription>
            The gateway ends the SIP leg (BYE) and closes the WebRTC leg. Both
            parties are disconnected immediately.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-1 rounded-md border border-border bg-muted/30 px-3 py-2 text-xs">
          {target?.label ? <p className="font-medium">{target.label}</p> : null}
          <p className="break-all font-mono text-muted-foreground">
            {target?.sessionId}
          </p>
        </div>
        {error ? (
          <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            size="sm"
            disabled={submitting}
            onClick={() => void submit()}
          >
            {submitting ? 'Hanging up…' : 'Hang up'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Sends RFC 4733 / SIP INFO DTMF digits into a live session. */
export function DtmfDialog({
  target,
  onClose,
}: {
  target: CallTarget | null
  onClose: () => void
}) {
  const [digits, setDigits] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (target) {
      setDigits('')
      setError(null)
    }
  }, [target])

  const submit = async () => {
    if (!target) return
    const invalid = validateDtmf(digits)
    if (invalid) {
      setError(invalid)
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const res = await sendSessionDtmf(target.sessionId, digits.trim())
      toast.success(`DTMF "${digits.trim()}" sent`, {
        description: res.message || undefined,
      })
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send DTMF')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Send DTMF</DialogTitle>
          <DialogDescription className="break-all font-mono text-xs">
            {target?.label ? `${target.label} · ` : ''}
            {target?.sessionId}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void submit()
          }}
          className="space-y-2"
        >
          <Input
            autoFocus
            value={digits}
            onChange={(e) => setDigits(e.target.value)}
            placeholder="e.g. 123#"
            className="font-mono text-sm"
            maxLength={DTMF_MAX_LEN}
            aria-label="DTMF digits"
          />
          <p className="text-[11px] text-muted-foreground">
            Digits 0-9, * and #. Up to {DTMF_MAX_LEN} characters.
          </p>
          {error ? (
            <p
              className="text-sm text-rose-600 dark:text-rose-400"
              role="alert"
            >
              {error}
            </p>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={submitting}>
              {submitting ? 'Sending…' : 'Send'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
