import { createFileRoute } from '@tanstack/react-router'
import { SessionHistoryPage } from '@/features/session-history/components/session-history-page'

export const Route = createFileRoute('/sessions/')({
  component: SessionHistoryPage,
})
