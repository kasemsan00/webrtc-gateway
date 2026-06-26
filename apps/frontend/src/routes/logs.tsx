import { createFileRoute } from '@tanstack/react-router'
import { GatewayLogsPage } from '@/features/gateway-logs/components/gateway-logs-page'

export const Route = createFileRoute('/logs')({
  component: GatewayLogsPage,
})