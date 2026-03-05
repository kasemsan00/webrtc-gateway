export interface KeycloakClientLike {
  authenticated?: boolean
  token?: string
  onTokenExpired?: () => void
  init: (options: {
    onLoad: 'login-required'
    pkceMethod: 'S256'
    checkLoginIframe: boolean
  }) => Promise<boolean>
  login: () => Promise<void>
  logout: () => Promise<void>
  updateToken: (minValidity: number) => Promise<boolean>
}

export interface KeycloakRuntimeState {
  ready: boolean
  authenticated: boolean
  token: string | null
}

interface InitializeKeycloakRuntimeOptions {
  client: KeycloakClientLike
  onStateChange: (state: KeycloakRuntimeState) => void
  onError: (error: Error) => void
  refreshIntervalMs?: number
}

export async function initializeKeycloakRuntime({
  client,
  onStateChange,
  onError,
  refreshIntervalMs = 15_000,
}: InitializeKeycloakRuntimeOptions): Promise<() => void> {
  let stopped = false

  const emitState = () => {
    onStateChange({
      ready: true,
      authenticated: Boolean(client.authenticated),
      token: client.token ?? null,
    })
  }

  const refreshToken = async () => {
    try {
      await client.updateToken(30)
      emitState()
    } catch (error) {
      if (stopped) return
      onError(error instanceof Error ? error : new Error('Token refresh failed'))
      await client.login()
    }
  }

  client.onTokenExpired = () => {
    void refreshToken()
  }

  try {
    const authenticated = await client.init({
      onLoad: 'login-required',
      pkceMethod: 'S256',
      checkLoginIframe: false,
    })

    if (!authenticated) {
      await client.login()
    }

    emitState()
  } catch (error) {
    throw error instanceof Error
      ? error
      : new Error('Failed to initialize Keycloak client')
  }

  const refreshTimer = setInterval(() => {
    void refreshToken()
  }, refreshIntervalMs)

  return () => {
    stopped = true
    clearInterval(refreshTimer)
  }
}
