import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateLogsSearch } from '@/lib/search-params'
import { GatewayLogsPage } from '@/features/gateway-logs/components/gateway-logs-page'

export const Route = createFileRoute('/logs')({
  head: routeHead('Gateway Logs'),
  validateSearch: validateLogsSearch,
  component: GatewayLogsPage,
})
