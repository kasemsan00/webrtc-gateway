import * as React from 'react'

import { cn } from '@/lib/utils'

function ScrollArea({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="scroll-area"
      className={cn(
        'overflow-auto scrollbar-thin scrollbar-thumb-white/20',
        className,
      )}
      {...props}
    />
  )
}

export { ScrollArea }
