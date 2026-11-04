import { Store } from '@tanstack/store'
import { useStore } from '@tanstack/react-store'
import { useEffect } from 'react'

import type { DetailedHealthResponse } from '@/features/dashboard/types'
import type { GatewayDashboard } from '@/features/gateway-instances/types'
import {
  fetchDetailedHealth,
  fetchGatewayHealth,
} from '@/features/dashboard/services/dashboard-api'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

export interface GatewayBuildInfo {
  version: string
  commit?: string
  branch?: string
  buildTime?: string
  dirty?: boolean
  goVersion?: string
}

export interface GatewayStatusState {
  build: GatewayBuildInfo | null
  dashboard: GatewayDashboard | null
  health: DetailedHealthResponse | null
  /** Last error from any status request; null once a request succeeds again. */
  error: string | null
  /** ms timestamp of the last successful health or dashboard fetch. */
  updatedAt: number | null
  loading: boolean
}

export const gatewayStatusStore = new Store<GatewayStatusState>({
  build: null,
  dashboard: null,
  health: null,
  error: null,
  updatedAt: null,
  loading: false,
})

const API_BASE = resolveGatewayApiBaseUrl()
const POLL_MS = 15_000

export async function fetchGatewayVersion(): Promise<GatewayBuildInfo> {
  return fetchJson<GatewayBuildInfo>(`${API_BASE}/version`)
}

let subscribers = 0
let timer: ReturnType<typeof setInterval> | null = null
let inFlight: Promise<void> | null = null

export function refreshGatewayStatus(): Promise<void> {
  if (inFlight) return inFlight
  gatewayStatusStore.setState((prev) => ({ ...prev, loading: true }))
  inFlight = (async () => {
    const needsBuild = !gatewayStatusStore.state.build
    const [dashboard, health, build] = await Promise.allSettled([
      fetchGatewayHealth(),
      fetchDetailedHealth(),
      needsBuild ? fetchGatewayVersion() : Promise.resolve(null),
    ])
    const errors: Array<string> = []
    for (const result of [dashboard, health]) {
      if (result.status === 'rejected') {
        errors.push(
          result.reason instanceof Error
            ? result.reason.message
            : String(result.reason),
        )
      }
    }
    gatewayStatusStore.setState((prev) => ({
      build:
        build.status === 'fulfilled' && build.value ? build.value : prev.build,
      dashboard:
        dashboard.status === 'fulfilled' ? dashboard.value : prev.dashboard,
      health: health.status === 'fulfilled' ? health.value : prev.health,
      error: errors.length ? errors[0] : null,
      updatedAt:
        dashboard.status === 'fulfilled' || health.status === 'fulfilled'
          ? Date.now()
          : prev.updatedAt,
      loading: false,
    }))
  })().finally(() => {
    inFlight = null
  })
  return inFlight
}

function start() {
  void refreshGatewayStatus()
  timer = setInterval(() => {
    if (document.visibilityState === 'visible') void refreshGatewayStatus()
  }, POLL_MS)
}

function stop() {
  if (timer) clearInterval(timer)
  timer = null
}

/**
 * Shared gateway status (build, instance, component health). Polling runs
 * while at least one component uses this hook, so the header and the
 * dashboard share one request stream.
 */
export function useGatewayStatus(): GatewayStatusState {
  useEffect(() => {
    subscribers += 1
    if (subscribers === 1) start()
    return () => {
      subscribers -= 1
      if (subscribers === 0) stop()
    }
  }, [])
  return useStore(gatewayStatusStore, (state) => state)
}
