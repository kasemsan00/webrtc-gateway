import type { VrsApiResponse, VrsConfig } from '../types'
import { fetchJson } from '@/lib/http-client'

const VRS_API_URL = 'https://vrswebapi.ttrs.in.th/extension/public'

export async function fetchVrsCredentials(
  config: VrsConfig,
): Promise<VrsApiResponse> {
  const data = await fetchJson<VrsApiResponse>(VRS_API_URL, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      type: 'mobile-public',
      agency: config.agency,
      phone: config.phone,
      fullName: config.fullName,
      emergency: config.emergency,
      emergency_options_data: config.emergencyOptionsData,
      user_agent: config.userAgent,
      mobileUID: config.mobileUID,
    }),
    timeoutMs: 12_000,
  })

  if (data.status !== 'OK') {
    throw new Error(`VRS API returned status: ${data.status}`)
  }

  return data
}
