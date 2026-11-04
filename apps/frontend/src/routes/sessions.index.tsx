import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateSessionsSearch } from '@/lib/search-params'
import { SessionHistoryPage } from '@/features/session-history/components/session-history-page'

export const Route = createFileRoute('/sessions/')({
  head: routeHead('Call Sessions'),
  validateSearch: validateSessionsSearch,
  component: SessionHistoryPage,
})
