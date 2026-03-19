import { createFileRoute } from '@tanstack/react-router'
import { SessionDirectoryPage } from '@/features/session-directory/components/session-directory-page'

export const Route = createFileRoute('/session-directory')({
  component: SessionDirectoryPage,
})
