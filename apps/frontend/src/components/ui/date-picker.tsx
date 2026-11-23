import { useState } from 'react'
import { RiCalendarLine } from '@remixicon/react'
import { format } from 'date-fns'

import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

const ISO_DATE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/

/** Parse a `YYYY-MM-DD` string into a local calendar Date without UTC shift. */
export function parseDateValue(value: string | undefined): Date | undefined {
  if (!value) return undefined
  const match = ISO_DATE_PATTERN.exec(value)
  if (!match) return undefined
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(year, month - 1, day)
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return undefined
  }
  return date
}

/** Format a local Date into a `YYYY-MM-DD` string. */
export function formatDateValue(date: Date): string {
  return format(date, 'yyyy-MM-dd')
}

export interface DatePickerProps {
  value?: string
  onChange: (value: string) => void
  placeholder?: string
  clearable?: boolean
  disabled?: boolean
  id?: string
  title?: string
  align?: 'start' | 'center' | 'end'
  className?: string
  'aria-label'?: string
}

export function DatePicker({
  value,
  onChange,
  placeholder = 'Pick a date',
  clearable = false,
  disabled = false,
  id,
  title,
  align = 'start',
  className,
  'aria-label': ariaLabel,
}: DatePickerProps) {
  const [open, setOpen] = useState(false)
  const selected = parseDateValue(value)

  const handleSelect = (next: Date | undefined) => {
    if (!next) {
      if (clearable) {
        onChange('')
        setOpen(false)
      }
      return
    }
    onChange(formatDateValue(next))
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          title={title}
          aria-label={ariaLabel}
          data-empty={!selected}
          className={cn(
            'h-8 justify-start gap-1.5 px-2.5 text-left text-xs font-normal tabular-nums data-[empty=true]:text-muted-foreground',
            className,
          )}
        >
          <RiCalendarLine className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">
            {selected ? format(selected, 'dd MMM yyyy') : placeholder}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align={align} className="w-auto gap-0 p-0">
        <Calendar
          mode="single"
          selected={selected}
          defaultMonth={selected}
          captionLayout="dropdown"
          onSelect={handleSelect}
          autoFocus
        />
        {clearable && selected ? (
          <div className="flex items-center justify-end border-t border-border px-3 py-2">
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={() => {
                onChange('')
                setOpen(false)
              }}
            >
              Clear
            </Button>
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}
