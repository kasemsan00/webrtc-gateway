import { createRouter } from '@tanstack/react-router'

import { routeTree } from './routeTree.gen'
import { routerBasePathFromEnv } from '@/lib/base-path'

// Import the generated route tree

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
