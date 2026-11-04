import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateSimpleSearch } from '@/lib/search-params'
import { ActiveSessionsPage } from '@/features/active-sessions/components/active-sessions-page'

export const Route = createFileRoute('/active-sessions')({
  head: routeHead('Active Sessions'),
  validateSearch: validateSimpleSearch,
  component: ActiveSessionsPage,
})
