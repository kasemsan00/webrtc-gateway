import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateSimpleSearch } from '@/lib/search-params'
import { WSClientsPage } from '@/features/ws-clients/components/ws-clients-page'

export const Route = createFileRoute('/ws-clients')({
  head: routeHead('WS Clients'),
  validateSearch: validateSimpleSearch,
  component: WSClientsPage,
})
