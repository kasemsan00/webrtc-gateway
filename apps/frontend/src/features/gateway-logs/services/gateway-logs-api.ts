import type { LogFileListResponse, LogTailResponse } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchLogFiles(): Promise<LogFileListResponse> {
  return fetchJson<LogFileListResponse>(`${API_BASE}/logs`)
}

export async function fetchLogTail(
  params: { name?: string; tail?: number } = {},
): Promise<LogTailResponse> {
  const path = params.name
    ? `${API_BASE}/logs/${encodeURIComponent(params.name)}`
    : `${API_BASE}/logs/current`
  const url = appendQuery(path, { tail: params.tail })
  return fetchJson<LogTailResponse>(url)
}