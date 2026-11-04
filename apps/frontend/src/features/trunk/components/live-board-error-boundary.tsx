import {
  RiErrorWarningFill,
  RiListCheck,
  RiRefreshLine,
} from '@remixicon/react'
import { Component } from 'react'
import type { ErrorInfo, ReactNode } from 'react'

import { Button } from '@/components/ui/button'

interface LiveBoardErrorBoundaryProps {
  children: ReactNode
  onSwitchToTable: () => void
}

interface LiveBoardErrorBoundaryState {
  error: Error | null
}

/**
 * Keeps a live board rendering bug from blanking the Trunks page: shows an
 * inline error with a way back to the Table view (which has all actions).
 */
export class LiveBoardErrorBoundary extends Component<
  LiveBoardErrorBoundaryProps,
  LiveBoardErrorBoundaryState
> {
  state: LiveBoardErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): LiveBoardErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[TrunkLiveBoard] render failed', error, info.componentStack)
  }

  private handleRetry = () => {
    this.setState({ error: null })
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children

    return (
      <div
        role="alert"
        className="flex flex-col gap-3 rounded-xl border border-l-4 border-l-rose-500 bg-card p-4"
      >
        <div className="flex items-start gap-2">
          <RiErrorWarningFill
            className="mt-0.5 size-5 shrink-0 text-rose-600 dark:text-rose-400"
            aria-hidden
          />
          <div className="min-w-0">
            <p className="text-sm font-semibold">
              The live board could not be displayed
            </p>
            <p className="text-sm break-words text-muted-foreground">
              {error.message || 'Unexpected error'}
            </p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={this.props.onSwitchToTable}>
            <RiListCheck className="size-4" aria-hidden />
            Switch to Table
          </Button>
          <Button size="sm" variant="outline" onClick={this.handleRetry}>
            <RiRefreshLine className="size-4" aria-hidden />
            Try again
          </Button>
        </div>
      </div>
    )
  }
}
