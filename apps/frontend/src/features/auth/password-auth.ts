import { clearAccessToken, setAccessToken } from './token-store'

export const ADMIN_PASSWORD_STORAGE_KEY = 'k2-admin-password'

export function persistAdminPassword(password: string): void {
  sessionStorage.setItem(ADMIN_PASSWORD_STORAGE_KEY, password)
  setAccessToken(password)
}

export function clearAdminSession(): void {
  sessionStorage.removeItem(ADMIN_PASSWORD_STORAGE_KEY)
  clearAccessToken()
}

export function restoreAdminPassword(): string | null {
  const stored = sessionStorage.getItem(ADMIN_PASSWORD_STORAGE_KEY)
  if (!stored) {
    clearAccessToken()
    return null
  }
  setAccessToken(stored)
  return stored
}
