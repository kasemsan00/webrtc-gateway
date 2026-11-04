import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateClientDiagnosticsSearch } from '@/lib/search-params'
import { ClientDiagnosticsPage } from '@/features/client-diagnostics/components/client-diagnostics-page'

export const Route = createFileRoute('/client-diagnostics')({
  head: routeHead('Client Diagnostics'),
  validateSearch: validateClientDiagnosticsSearch,
  component: ClientDiagnosticsPage,
})
