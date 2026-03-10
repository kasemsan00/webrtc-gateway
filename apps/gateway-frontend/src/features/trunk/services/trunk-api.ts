import type {
  CreateTrunkPayload,
  Trunk,
  TrunkListParams,
  TrunkListResponse,
  UpdateTrunkPayload,
} from '../types'
import {
  buildAuthHeaders,
  fetchJson,
  resolveGatewayApiBaseUrl,
} from '@/lib/http-client'
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

export async function deleteTrunk(
  id: number,
): Promise<{ trunkId: number; status: string }> {
  return fetchJson<{ trunkId: number; status: string }>(
    `${API_BASE}/trunk/${id}`,
    {
      method: 'DELETE',
    },
  )
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
  const controller = new AbortController()
  const decoder = new TextDecoder()
  let buffer = ''

  const handleChunk = (chunk: string) => {
    buffer += chunk.replace(/\r\n/g, '\n')

    for (;;) {
      const eventBoundary = buffer.indexOf('\n\n')
      if (eventBoundary === -1) break

      const rawEvent = buffer.slice(0, eventBoundary)
      buffer = buffer.slice(eventBoundary + 2)

      const lines = rawEvent.split(/\r?\n/)
      let eventName = 'message'
      const dataLines: Array<string> = []

      for (const line of lines) {
        if (line.startsWith('event:')) {
          eventName = line.slice('event:'.length).trim()
          continue
        }

        if (line.startsWith('data:')) {
          dataLines.push(line.slice('data:'.length).trim())
        }
      }

      if (eventName !== 'trunk' || dataLines.length === 0) continue

      try {
        const parsed = JSON.parse(dataLines.join('\n')) as TrunkStreamEvent
        onEvent(parsed)
      } catch {
        // Ignore malformed event payloads.
      }
    }
  }

  const start = async () => {
    try {
      const response = await fetch(`${API_BASE}/trunks/stream`, {
        method: 'GET',
        headers: buildAuthHeaders({
          Accept: 'text/event-stream',
        }),
        signal: controller.signal,
      })

      if (!response.ok || !response.body) {
        throw new Error(
          `Failed to subscribe trunk events: HTTP ${response.status}`,
        )
      }

      const reader = response.body.getReader()
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break

        handleChunk(decoder.decode(value, { stream: true }))
      }
    } catch {
      if (!controller.signal.aborted && onError) {
        onError(new Event('error'))
      }
    }
  }

  void start()

  return () => {
    controller.abort()
  }
}
