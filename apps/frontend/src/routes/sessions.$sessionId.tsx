import { createFileRoute } from '@tanstack/react-router'
import { documentTitle } from '@/lib/navigation'
import { validateSessionDetailSearch } from '@/lib/search-params'
import { SessionDetailPage } from '@/features/session-detail/components/session-detail-page'

export const Route = createFileRoute('/sessions/$sessionId')({
  head: ({ params }) => ({
    meta: [
      { title: documentTitle(`Session ${params.sessionId.slice(0, 12)}`) },
    ],
  }),
  validateSearch: validateSessionDetailSearch,
  component: SessionDetailPage,
})
