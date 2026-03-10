import { createFileRoute } from '@tanstack/react-router'
import { GatewayInstancesPage } from '@/features/gateway-instances/components/gateway-instances-page'

export const Route = createFileRoute('/instances')({
  component: GatewayInstancesPage,
})
