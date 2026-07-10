import { createFileRoute } from '@tanstack/react-router'
import { DashboardPage } from '@/features/dashboard/components/dashboard-page'

export const Route = createFileRoute('/')({
  component: DashboardPage,
})
