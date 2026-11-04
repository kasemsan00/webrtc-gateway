import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateSimpleSearch } from '@/lib/search-params'
import { SessionDirectoryPage } from '@/features/session-directory/components/session-directory-page'

export const Route = createFileRoute('/session-directory')({
  head: routeHead('Session Directory'),
  validateSearch: validateSimpleSearch,
  component: SessionDirectoryPage,
})
