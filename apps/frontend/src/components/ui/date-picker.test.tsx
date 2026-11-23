import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DatePicker, formatDateValue, parseDateValue } from './date-picker'

afterEach(() => {
  cleanup()
})

describe('DatePicker helpers', () => {
  it('parses valid YYYY-MM-DD strings without timezone shift', () => {
    const parsed = parseDateValue('2026-11-20')
    expect(parsed).toBeInstanceOf(Date)
    expect(parsed?.getFullYear()).toBe(2026)
    expect(parsed?.getMonth()).toBe(10)
    expect(parsed?.getDate()).toBe(20)
    expect(formatDateValue(parsed!)).toBe('2026-11-20')
  })

  it('rejects empty and impossible calendar dates', () => {
    expect(parseDateValue('')).toBeUndefined()
    expect(parseDateValue(undefined)).toBeUndefined()
    expect(parseDateValue('2026-02-31')).toBeUndefined()
    expect(parseDateValue('not-a-date')).toBeUndefined()
  })
})

describe('DatePicker component', () => {
  it('renders formatted value and calls onChange with YYYY-MM-DD when a day is picked', () => {
    const onChange = vi.fn()
    render(
      <DatePicker
        value="2026-11-20"
        onChange={onChange}
        aria-label="Anchor date"
      />,
    )

    const trigger = screen.getByRole('button', { name: 'Anchor date' })
    expect(trigger.textContent).toContain('20 Nov 2026')

    fireEvent.click(trigger)
    const day15 = screen.getByRole('button', { name: /November 15/i })
    fireEvent.click(day15)

    expect(onChange).toHaveBeenCalledWith('2026-11-15')
  })

  it('supports clearing when clearable is enabled', () => {
    const onChange = vi.fn()
    render(
      <DatePicker
        value="2026-11-20"
        onChange={onChange}
        clearable
        placeholder="From date"
        aria-label="From date"
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'From date' }))
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))

    expect(onChange).toHaveBeenCalledWith('')
  })

  it('shows placeholder when empty', () => {
    render(
      <DatePicker
        value=""
        onChange={vi.fn()}
        placeholder="Select date"
        aria-label="Empty date"
      />,
    )

    const trigger = screen.getByRole('button', { name: 'Empty date' })
    expect(trigger.textContent).toContain('Select date')
    expect(trigger.getAttribute('data-empty')).toBe('true')
  })
})
