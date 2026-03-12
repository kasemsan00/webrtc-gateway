import type { PublicAccount } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchPublicAccounts(): Promise<Array<PublicAccount>> {
  return fetchJson<Array<PublicAccount>>(`${API_BASE}/public-accounts`)
}
