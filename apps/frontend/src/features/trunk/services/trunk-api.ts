import type {
  CreateTrunkPayload,
  TestIncomingPushResponse,
  TestIncomingPushStyle,
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

/** Largest page the gateway accepts for GET /api/trunks. */
export const TRUNK_API_MAX_PAGE_SIZE = 100
const FETCH_ALL_TRUNKS_MAX_PAGES = 50

/**
 * Fetches every trunk matching `params` by walking pages of the API maximum.
 * Used by the live board, which shows all trunks without pagination.
 */
export async function fetchAllTrunks(
  params: Omit<TrunkListParams, 'page' | 'pageSize'> = {},
): Promise<TrunkListResponse> {
  const items: Array<Trunk> = []
  let total = 0
  for (let page = 1; page <= FETCH_ALL_TRUNKS_MAX_PAGES; page += 1) {
    const res = await fetchTrunks({
      ...params,
      page,
      pageSize: TRUNK_API_MAX_PAGE_SIZE,
    })
    // The gateway may send `items: null` for an empty page.
    const pageItems: Array<Trunk> = Array.isArray(res.items) ? res.items : []
    items.push(...pageItems)
    total = typeof res.total === 'number' ? res.total : items.length
    if (pageItems.length === 0 || items.length >= total) break
  }
  return { items, total, page: 1, pageSize: items.length }
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

export async function testIncomingPush(
  id: number,
  style: TestIncomingPushStyle = 'data',
): Promise<TestIncomingPushResponse> {
  return fetchJson<TestIncomingPushResponse>(
    `${API_BASE}/trunk/${id}/test-incoming-push`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ style }),
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
