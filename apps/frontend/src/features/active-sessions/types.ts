export interface ActiveSession {
  id: string
  state: string
  direction: string
  from: string
  to: string
  sipCallId: string
  authMode: string
  trunkId: number
  trunkName: string
  sipUsername: string
  durationSec: number
  createdAt: string
  /** RFC3339 time the call was first answered; null while ringing. */
  answeredAt?: string | null
  updatedAt: string
  translatorEnabled: boolean
  translatorSrcLang?: string
  translatorTgtLang?: string
  translatorTtsVoice?: string
}

export interface ActiveSessionsListResponse {
  items: Array<ActiveSession>
  total: number
  page: number
  pageSize: number
}

export interface ActiveSessionsListParams {
  page?: number
  pageSize?: number
}
