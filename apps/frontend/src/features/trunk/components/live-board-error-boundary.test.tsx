// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { LiveBoardErrorBoundary } from './live-board-error-boundary'

function Boom(): never {
  throw new TypeError("Cannot read properties of undefined (reading 'trim')")
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('LiveBoardErrorBoundary', () => {
  it('shows an inline error with a Switch to Table button', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const onSwitchToTable = vi.fn()

    render(
      <LiveBoardErrorBoundary onSwitchToTable={onSwitchToTable}>
        <Boom />
      </LiveBoardErrorBoundary>,
    )

    expect(screen.getByRole('alert').textContent).toContain("reading 'trim'")
    fireEvent.click(screen.getByRole('button', { name: /switch to table/i }))
    expect(onSwitchToTable).toHaveBeenCalledTimes(1)
  })

  it('renders children when nothing throws', () => {
    render(
      <LiveBoardErrorBoundary onSwitchToTable={() => {}}>
        <p>board</p>
      </LiveBoardErrorBoundary>,
    )
    expect(screen.getByText('board')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })
})
