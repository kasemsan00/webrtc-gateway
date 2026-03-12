import { createFileRoute } from '@tanstack/react-router'
import { TrunkListPage } from '@/features/trunk/components/trunk-list-page'

export const Route = createFileRoute('/trunks')({
  component: TrunkListPage,
})
