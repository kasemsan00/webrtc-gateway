import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'

import { clearAccessToken, setAccessToken } from './token-store'
import { getKeycloakRuntimeConfig } from './runtime-env'
import { initializeKeycloakRuntime } from './keycloak-runtime'
import type {
  KeycloakClientLike,
  KeycloakRuntimeState,
} from './keycloak-runtime'

interface AuthContextValue extends KeycloakRuntimeState {
  login: () => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

const browser = typeof window !== 'undefined'

const initialState: KeycloakRuntimeState = {
  ready: false,
  authenticated: false,
  token: null,
}

async function createKeycloakClient(): Promise<KeycloakClientLike> {
  const config = getKeycloakRuntimeConfig()
  const { default: Keycloak } = await import('keycloak-js')

  return new Keycloak({
    url: config.url,
    realm: config.realm,
    clientId: config.clientId,
  }) as KeycloakClientLike
}

export function KeycloakAuthProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [state, setState] = useState<KeycloakRuntimeState>(initialState)
  const [mounted, setMounted] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const keycloakRef = useRef<KeycloakClientLike | null>(null)

  useEffect(() => {
    if (!browser) return

    setMounted(true)
  }, [])

  useEffect(() => {
    if (!mounted) return

    let cleanup: (() => void) | undefined
    let unmounted = false

    const bootstrap = async () => {
      try {
        const keycloak = await createKeycloakClient()
        if (unmounted) return

        keycloakRef.current = keycloak

        cleanup = await initializeKeycloakRuntime({
          client: keycloak,
          onStateChange: (nextState) => {
            setAccessToken(nextState.token)
            setState(nextState)
          },
          onError: (runtimeError) => {
            setError(runtimeError.message)
          },
        })
      } catch (bootstrapError) {
        if (unmounted) return
        setError(
          bootstrapError instanceof Error
            ? bootstrapError.message
            : 'Failed to initialize authentication',
        )
      }
    }

    void bootstrap()

    return () => {
      unmounted = true
      cleanup?.()
      keycloakRef.current = null
      clearAccessToken()
    }
  }, [mounted])

  const login = useCallback(async () => {
    const keycloak = keycloakRef.current
    if (!keycloak) return
    await keycloak.login()
  }, [])

  const logout = useCallback(async () => {
    const keycloak = keycloakRef.current
    if (!keycloak) return
    await keycloak.logout()
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      ...state,
      login,
      logout,
    }),
    [state, login, logout],
  )

  if (!mounted) {
    return <>{children}</>
  }

  if (error) {
    return (
      <div className="flex h-screen items-center justify-center bg-background px-6 text-foreground">
        <div className="max-w-md rounded-md border border-red-500/40 bg-red-500/10 p-4 text-sm text-red-300">
          Authentication initialization failed: {error}
        </div>
      </div>
    )
  }

  if (!state.ready || !state.authenticated) {
    return (
      <div className="flex h-screen items-center justify-center bg-background text-sm text-muted-foreground">
        Authenticating...
      </div>
    )
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useKeycloakAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useKeycloakAuth must be used within KeycloakAuthProvider')
  }
  return context
}
