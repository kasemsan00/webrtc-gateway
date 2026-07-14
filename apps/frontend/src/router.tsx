import { createRouter } from '@tanstack/react-router'

import { routerBasePathFromEnv } from '@/lib/base-path'

// Import the generated route tree
import { routeTree } from './routeTree.gen'

// Create a new router instance
export const getRouter = () => {
  const basepath = routerBasePathFromEnv(import.meta.env.VITE_BASE_PATH)

  const router = createRouter({
    routeTree,
    context: {},
    ...(basepath ? { basepath } : {}),
    scrollRestoration: true,
    defaultPreloadStaleTime: 0,
  })

  return router
}
