import { createFileRoute } from '@tanstack/react-router'

import { MultiStreamTestPage } from '@/features/multi-stream-test/components/multi-stream-test-page'
import { routeHead } from '@/lib/navigation'

export const Route = createFileRoute('/multi-stream-test')({
  head: routeHead('Multi-Stream Test'),
  component: MultiStreamTestPage,
})
