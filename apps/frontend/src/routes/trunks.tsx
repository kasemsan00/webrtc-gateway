import { createFileRoute } from '@tanstack/react-router'
import { TrunkListPage } from '@/features/trunk/components/trunk-list-page'

export const Route = createFileRoute('/trunks')({
  validateSearch: (search: Record<string, unknown>) => ({
    search: typeof search.search === 'string' ? search.search : undefined,
    trunkPublicId:
      typeof search.trunkPublicId === 'string'
        ? search.trunkPublicId
        : undefined,
    trunkId:
      typeof search.trunkId === 'number'
        ? search.trunkId
        : typeof search.trunkId === 'string' && /^\d+$/.test(search.trunkId)
          ? Number(search.trunkId)
          : undefined,
  }),
  component: TrunkListPage,
})
