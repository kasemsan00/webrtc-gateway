import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateSimpleSearch } from '@/lib/search-params'
import { GatewayInstancesPage } from '@/features/gateway-instances/components/gateway-instances-page'

export const Route = createFileRoute('/instances')({
  head: routeHead('Instances'),
  validateSearch: validateSimpleSearch,
  component: GatewayInstancesPage,
})
