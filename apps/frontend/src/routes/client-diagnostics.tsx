import { createFileRoute } from '@tanstack/react-router'
import { ClientDiagnosticsPage } from '@/features/client-diagnostics/components/client-diagnostics-page'

export const Route = createFileRoute('/client-diagnostics')({
  component: ClientDiagnosticsPage,
})