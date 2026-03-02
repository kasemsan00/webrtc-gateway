import { RiArrowLeftSLine, RiArrowRightSLine } from '@remixicon/react'
import { useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const MAX_PAGE_SIZE = 100

function clampPage(page: number, totalPages: number) {
  if (!Number.isFinite(page)) return 1
  return Math.min(Math.max(1, page), Math.max(1, totalPages))
}

function clampPageSize(pageSize: number) {
  if (!Number.isFinite(pageSize) || pageSize < 1) return 20
  return Math.min(Math.floor(pageSize), MAX_PAGE_SIZE)
}

export type ServerPaginationControlsProps = {
  page: number
  totalPages: number
  total: number
  pageSize: number
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  pageSizeOptions?: Array<number>
  totalLabel?: string
}

export function ServerPaginationControls({
  page,
  totalPages,
  total,
  pageSize,
  onPageChange,
  onPageSizeChange,
  pageSizeOptions = [20, 50, 100],
  totalLabel = 'total',
}: ServerPaginationControlsProps) {
  const [pageInput, setPageInput] = useState(String(page))

  useEffect(() => {
    setPageInput(String(page))
  }, [page])

  const normalizedPage = clampPage(page, totalPages)
  const normalizedPageSize = clampPageSize(pageSize)
  const canPrev = normalizedPage > 1
  const canNext = normalizedPage < Math.max(1, totalPages)

  const normalizedOptions = useMemo(() => {
    const safeOptions = pageSizeOptions
      .map((value) => clampPageSize(value))
      .filter((value) => value > 0)
    if (!safeOptions.includes(normalizedPageSize)) {
      safeOptions.push(normalizedPageSize)
    }
    return Array.from(new Set(safeOptions)).sort((a, b) => a - b)
  }, [normalizedPageSize, pageSizeOptions])

  const commitPageInput = () => {
    const parsed = Number(pageInput)
    const nextPage = clampPage(parsed, totalPages)
    setPageInput(String(nextPage))
    if (nextPage !== normalizedPage) {
      onPageChange(nextPage)
    }
  }

  return (
    <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
      <Button
        size="sm"
        variant="outline"
        className="h-7 gap-1 px-2 text-xs"
        disabled={!canPrev}
        onClick={() => onPageChange(normalizedPage - 1)}
      >
        <RiArrowLeftSLine className="size-3.5" />
        Prev
      </Button>

      <span className="text-xs text-muted-foreground">Page</span>
      <Input
        className="h-7 w-16 text-center text-xs"
        value={pageInput}
        inputMode="numeric"
        onChange={(e) => setPageInput(e.target.value.replace(/[^\d]/g, ''))}
        onBlur={commitPageInput}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            commitPageInput()
          }
        }}
      />
      <span className="text-xs text-muted-foreground">/ {totalPages}</span>

      <Button
        size="sm"
        variant="outline"
        className="h-7 gap-1 px-2 text-xs"
        disabled={!canNext}
        onClick={() => onPageChange(normalizedPage + 1)}
      >
        Next
        <RiArrowRightSLine className="size-3.5" />
      </Button>

      <span className="ml-1 text-xs text-muted-foreground">Rows</span>
      <Select
        value={String(normalizedPageSize)}
        onValueChange={(value) =>
          onPageSizeChange(clampPageSize(Number(value)))
        }
      >
        <SelectTrigger className="h-7 w-20 px-2 text-xs" size="sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {normalizedOptions.map((size) => (
            <SelectItem key={size} value={String(size)}>
              {size}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <span className="text-xs text-muted-foreground">
        ({total} {totalLabel})
      </span>
    </div>
  )
}
