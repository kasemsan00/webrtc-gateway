import { createFileRoute } from '@tanstack/react-router'
import { GatewayConfigPage } from '@/features/gateway-config/components/gateway-config-page'

export const Route = createFileRoute('/settings')({
  component: GatewayConfigPage,
})
