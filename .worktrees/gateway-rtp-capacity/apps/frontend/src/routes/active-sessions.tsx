import { createFileRoute } from '@tanstack/react-router'
import { ActiveSessionsPage } from '@/features/active-sessions/components/active-sessions-page'

export const Route = createFileRoute('/active-sessions')({
  component: ActiveSessionsPage,
})
