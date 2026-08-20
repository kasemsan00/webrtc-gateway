import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getAccessToken } from './token-store'
import {
  ADMIN_PASSWORD_STORAGE_KEY,
  LEGACY_ADMIN_PASSWORD_STORAGE_KEY,
  clearAdminSession,
  restoreAdminPassword,
} from './password-auth'
import { passwordsMatch } from './password-match'
import { PasswordAuthProvider, usePasswordAuth } from './password-provider'

afterEach(() => {
  cleanup()
  sessionStorage.clear()
  clearAdminSession()
})

function LogoutButton() {
  const { logout } = usePasswordAuth()
  return (
    <button type="button" onClick={logout}>
      Logout
    </button>
  )
}

function DashboardStub() {
  return (
    <div>
      <p>Dashboard</p>
      <LogoutButton />
    </div>
  )
}

describe('passwordsMatch', () => {
  it('accepts equal secrets', () => {
    expect(passwordsMatch('ops-secret', 'ops-secret')).toBe(true)
  })

  it('rejects different secrets', () => {
    expect(passwordsMatch('ops-secret', 'other')).toBe(false)
  })
})

describe('PasswordAuthProvider', () => {
  it('migrates the legacy password session without overriding a current session', () => {
    sessionStorage.setItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY, 'legacy-secret')

    expect(restoreAdminPassword()).toBe('legacy-secret')
    expect(sessionStorage.getItem(ADMIN_PASSWORD_STORAGE_KEY)).toBe(
      'legacy-secret',
    )
    expect(sessionStorage.getItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY)).toBeNull()

    sessionStorage.setItem(ADMIN_PASSWORD_STORAGE_KEY, 'current-secret')
    sessionStorage.setItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY, 'older-secret')
    expect(restoreAdminPassword()).toBe('current-secret')
    expect(sessionStorage.getItem(LEGACY_ADMIN_PASSWORD_STORAGE_KEY)).toBeNull()
  })

  it('shows operations pages after a successful login and stores the bearer', async () => {
    const verifyPassword = vi.fn().mockResolvedValue(true)

    render(
      <PasswordAuthProvider verifyPassword={verifyPassword}>
        <DashboardStub />
      </PasswordAuthProvider>,
    )

    fireEvent.change(await screen.findByPlaceholderText('Password'), {
      target: { value: 'ops-secret' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('Dashboard')).toBeTruthy()
    expect(verifyPassword).toHaveBeenCalledWith('ops-secret')
    expect(getAccessToken()).toBe('ops-secret')
    expect(sessionStorage.getItem(ADMIN_PASSWORD_STORAGE_KEY)).toBe(
      'ops-secret',
    )
  })

  it('stays on the login form when the password is wrong', async () => {
    const verifyPassword = vi.fn().mockResolvedValue(false)

    render(
      <PasswordAuthProvider verifyPassword={verifyPassword}>
        <DashboardStub />
      </PasswordAuthProvider>,
    )

    fireEvent.change(await screen.findByPlaceholderText('Password'), {
      target: { value: 'wrong' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect((await screen.findByRole('alert')).textContent).toBe(
      'Invalid password',
    )
    expect(screen.queryByText('Dashboard')).toBeNull()
    expect(getAccessToken()).toBeNull()
  })

  it('clears the bearer on logout', async () => {
    const verifyPassword = vi.fn().mockResolvedValue(true)

    render(
      <PasswordAuthProvider verifyPassword={verifyPassword}>
        <DashboardStub />
      </PasswordAuthProvider>,
    )

    fireEvent.change(await screen.findByPlaceholderText('Password'), {
      target: { value: 'ops-secret' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('Dashboard')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Logout' }))

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Password')).toBeTruthy()
    })
    expect(getAccessToken()).toBeNull()
    expect(sessionStorage.getItem(ADMIN_PASSWORD_STORAGE_KEY)).toBeNull()
  })
})
