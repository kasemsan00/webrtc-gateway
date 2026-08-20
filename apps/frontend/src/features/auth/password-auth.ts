import { clearAccessToken, setAccessToken } from './token-store'

export const ADMIN_PASSWORD_STORAGE_KEY = 'webrtc-sip-gateway-admin-password'
export const LEGACY_ADMIN_PASSWORD_STORAGE_KEY = 'k2-admin-password'

export function persistAdminPassword(password: string): void {
  sessionStorage.setItem(ADMIN_PASSWORD_STORAGE_KEY, password)
  setAccessToken(password)
}

export function clearAdminSession(): void {
  sessionStorage.removeItem(ADMIN_PASSWORD_STORAGE_KEY)
  sessionStorage.removeItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY)
  clearAccessToken()
}

export function restoreAdminPassword(): string | null {
  const current = sessionStorage.getItem(ADMIN_PASSWORD_STORAGE_KEY)
  if (current) {
    sessionStorage.removeItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY)
    setAccessToken(current)
    return current
  }

  const legacy = sessionStorage.getItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY)
  if (legacy) {
    sessionStorage.setItem(ADMIN_PASSWORD_STORAGE_KEY, legacy)
    sessionStorage.removeItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY)
    setAccessToken(legacy)
    return legacy
  }

  const stored = current
  if (!stored) {
    clearAccessToken()
    return null
  }
  setAccessToken(stored)
  return stored
}
