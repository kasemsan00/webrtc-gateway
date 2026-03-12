import { createFileRoute } from '@tanstack/react-router'
import { SessionDetailPage } from '@/features/session-detail/components/session-detail-page'

export const Route = createFileRoute('/sessions/$sessionId')({
  component: SessionDetailPage,
})
