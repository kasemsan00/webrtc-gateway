import { createFileRoute } from '@tanstack/react-router'
import { GatewayInstancesPage } from '@/features/gateway-instances/components/gateway-instances-page'

export const Route = createFileRoute('/instances')({
  validateSearch: (search: Record<string, unknown>) => ({
    search: typeof search.search === 'string' ? search.search : undefined,
  }),
  component: GatewayInstancesPage,
})
