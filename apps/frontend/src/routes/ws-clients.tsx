import { createFileRoute } from '@tanstack/react-router'
import { WSClientsPage } from '@/features/ws-clients/components/ws-clients-page'

export const Route = createFileRoute('/ws-clients')({
  component: WSClientsPage,
})
