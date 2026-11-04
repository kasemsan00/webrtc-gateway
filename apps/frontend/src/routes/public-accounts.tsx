import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { validateSimpleSearch } from '@/lib/search-params'
import { PublicAccountsPage } from '@/features/public-accounts/components/public-accounts-page'

export const Route = createFileRoute('/public-accounts')({
  head: routeHead('Public Accounts'),
  validateSearch: validateSimpleSearch,
  component: PublicAccountsPage,
})
