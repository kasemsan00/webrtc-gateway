import type {
  CreateTrunkPayload,
  Trunk,
  TrunkListParams,
  TrunkListResponse,
  UpdateTrunkPayload,
} from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { subscribeAuthenticatedSse } from '@/lib/sse-subscriber'
import { appendQuery } from '@/lib/http-query'

export interface TrunkStreamEvent {
  type: string
  trunkId?: number
  at: string
}

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchTrunks(
  params: TrunkListParams = {},
): Promise<TrunkListResponse> {
  const url = appendQuery(`${API_BASE}/trunks`, {
    page: params.page,
    pageSize: params.pageSize,
    trunkId: params.trunkId,
    trunkPublicId: params.trunkPublicId,
    search: params.search,
    createdAfter: params.createdAfter,
    createdBefore: params.createdBefore,
    sortBy: params.sortBy,
    sortDir: params.sortDir,
  })
  return fetchJson<TrunkListResponse>(url)
}

export async function refreshTrunks(): Promise<{ status: string }> {
  return fetchJson<{ status: string }>(`${API_BASE}/trunks/refresh`, {
    method: 'POST',
  })
}

export async function softDeleteTrunk(id: number): Promise<Trunk> {
  return updateTrunk(id, { enabled: false })
}

export async function restoreTrunk(id: number): Promise<Trunk> {
  return updateTrunk(id, { enabled: true })
}

export async function unregisterTrunk(
  id: number,
): Promise<{ trunkId: number; status: string }> {
  return fetchJson<{ trunkId: number; status: string }>(
    `${API_BASE}/trunk/${id}/unregister`,
    {
      method: 'POST',
    },
  )
}

export async function registerTrunk(
  id: number,
): Promise<{ trunkId: number; status: string }> {
  return fetchJson<{ trunkId: number; status: string }>(
    `${API_BASE}/trunk/${id}/register`,
    {
      method: 'POST',
    },
  )
}

export async function updateTrunk(
  id: number,
  payload: UpdateTrunkPayload,
): Promise<Trunk> {
  return fetchJson<Trunk>(`${API_BASE}/trunk/${id}`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}

export async function createTrunk(payload: CreateTrunkPayload): Promise<Trunk> {
  return fetchJson<Trunk>(`${API_BASE}/trunks`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}

export function subscribeTrunkEvents(
  onEvent: (event: TrunkStreamEvent) => void,
  onError?: (event: Event) => void,
) {
  return subscribeAuthenticatedSse<TrunkStreamEvent>({
    url: `${API_BASE}/trunks/stream`,
    eventName: 'trunk',
    onEvent,
    onError,
  })
}
