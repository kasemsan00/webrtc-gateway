export interface PublicAccount {
  key: string
  domain: string
  port: number
  username: string
  isRegistered: boolean
  refCountActiveCalls: number
  lastUsedAt: string
  expiresAt: string
  lastError: string
}
