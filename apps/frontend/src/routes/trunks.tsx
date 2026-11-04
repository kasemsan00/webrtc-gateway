import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateTrunksSearch } from '@/lib/search-params'
import { TrunkListPage } from '@/features/trunk/components/trunk-list-page'

export const Route = createFileRoute('/trunks')({
  head: routeHead('Trunks'),
  validateSearch: validateTrunksSearch,
  component: TrunkListPage,
})
