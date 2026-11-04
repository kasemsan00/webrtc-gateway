import { createFileRoute } from '@tanstack/react-router'
import { routeHead } from '@/lib/navigation'
import { DashboardPage } from '@/features/dashboard/components/dashboard-page'

export const Route = createFileRoute('/')({
  head: routeHead('Dashboard'),
  component: DashboardPage,
})
