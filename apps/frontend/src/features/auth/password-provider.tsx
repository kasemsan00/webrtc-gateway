import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  clearAdminSession,
  persistAdminPassword,
  restoreAdminPassword,
} from './password-auth'

interface PasswordAuthContextValue {
  authenticated: boolean
  login: (password: string) => Promise<boolean>
  logout: () => void
}

const PasswordAuthContext = createContext<PasswordAuthContextValue | null>(null)

const browser = typeof window !== 'undefined'

export function PasswordAuthProvider({
  children,
  verifyPassword,
}: {
  children: React.ReactNode
  verifyPassword: (password: string) => Promise<boolean>
}) {
  const [mounted, setMounted] = useState(false)
  const [authenticated, setAuthenticated] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)

  useEffect(() => {
    if (!browser) return
    setMounted(true)
    setAuthenticated(restoreAdminPassword() !== null)
  }, [])

  const login = useCallback(
    async (password: string) => {
      setError(null)
      setIsSubmitting(true)
      try {
        const ok = await verifyPassword(password)
        if (!ok) {
          setError('Invalid password')
          return false
        }
        persistAdminPassword(password)
        setAuthenticated(true)
        return true
      } catch (loginError) {
        setError(
          loginError instanceof Error
            ? loginError.message
            : 'Login failed',
        )
        return false
      } finally {
        setIsSubmitting(false)
      }
    },
    [verifyPassword],
  )

  const logout = useCallback(() => {
    clearAdminSession()
    setAuthenticated(false)
    setError(null)
  }, [])

  const value = useMemo<PasswordAuthContextValue>(
    () => ({
      authenticated,
      login,
      logout,
    }),
    [authenticated, login, logout],
  )

  if (!mounted) {
    return (
      <div className="flex h-screen items-center justify-center bg-background text-sm text-muted-foreground">
        Loading...
      </div>
    )
  }

  if (!authenticated) {
    return (
      <PasswordAuthContext.Provider value={value}>
        <LoginForm
          error={error}
          isSubmitting={isSubmitting}
          onSubmit={login}
        />
      </PasswordAuthContext.Provider>
    )
  }

  return (
    <PasswordAuthContext.Provider value={value}>{children}</PasswordAuthContext.Provider>
  )
}

function LoginForm({
  error,
  isSubmitting,
  onSubmit,
}: {
  error: string | null
  isSubmitting: boolean
  onSubmit: (password: string) => Promise<boolean>
}) {
  const [password, setPassword] = useState('')

  return (
    <div className="flex h-screen items-center justify-center bg-background px-6 text-foreground">
      <form
        className="w-full max-w-sm space-y-4 rounded-md border border-border bg-card p-6"
        onSubmit={(event) => {
          event.preventDefault()
          void onSubmit(password)
        }}
      >
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">WebRTC-SIP Gateway Console</h1>
          <p className="text-sm text-muted-foreground">
            Enter the admin password to continue.
          </p>
        </div>
        <Input
          type="password"
          autoComplete="current-password"
          placeholder="Password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          disabled={isSubmitting}
        />
        {error ? (
          <p className="text-sm text-red-400" role="alert">
            {error}
          </p>
        ) : null}
        <Button type="submit" className="w-full" disabled={isSubmitting}>
          {isSubmitting ? 'Signing in...' : 'Sign in'}
        </Button>
      </form>
    </div>
  )
}

export function usePasswordAuth(): PasswordAuthContextValue {
  const context = useContext(PasswordAuthContext)
  if (!context) {
    throw new Error('usePasswordAuth must be used within PasswordAuthProvider')
  }
  return context
}
