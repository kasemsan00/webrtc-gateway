import { createFileRoute } from '@tanstack/react-router'
import { PublicAccountsPage } from '@/features/public-accounts/components/public-accounts-page'

export const Route = createFileRoute('/public-accounts')({
  component: PublicAccountsPage,
})
