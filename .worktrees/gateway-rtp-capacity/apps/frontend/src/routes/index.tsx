import { createFileRoute } from '@tanstack/react-router'
import { GatewayConsolePage } from '@/features/gateway/components/gateway-console-page'

export const Route = createFileRoute('/')({
  component: GatewayConsolePage,
})
