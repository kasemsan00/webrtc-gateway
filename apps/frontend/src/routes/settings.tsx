import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { GatewayConfigPage } from '@/features/gateway-config/components/gateway-config-page'

export const Route = createFileRoute('/settings')({
  head: routeHead('Configuration'),
  component: GatewayConfigPage,
})
