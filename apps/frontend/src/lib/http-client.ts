import { getAccessToken } from '@/features/auth/token-store'
import { readAppEnvValue } from './runtime-env'

const DEFAULT_TIMEOUT_MS = 15_000

const DEFAULT_GATEWAY_PORT = '8000'

const isBrowser = () => typeof window !== 'undefined'

function resolveFallbackGatewayOrigin(): string {
  if (!isBrowser()) {
    return `http://localhost:${DEFAULT_GATEWAY_PORT}`
  }

  if (window.location.hostname === 'k2-gateway.kasemsan.com') {
    return 'https://k2-gateway.kasemsan.com'
  }

  const protocol = window.location.protocol === 'https:' ? 'https' : 'http'
  return `${protocol}://${window.location.hostname}:${DEFAULT_GATEWAY_PORT}`
}

type ResolveGatewayApiBaseUrlOptions = {
  envUrlOverride?: string
  skipEnv?: boolean
}

export function resolveGatewayApiBaseUrl(
  options: ResolveGatewayApiBaseUrlOptions = {},
): string {
  const overrideUrl = options.envUrlOverride?.trim()
  const envUrl = overrideUrl
    ? overrideUrl
    : options.skipEnv
      ? undefined
      : readAppEnvValue('VITE_GATEWAY_URL')

  const normalizedOrigin = envUrl
    ? /^https?:\/\//i.test(envUrl)
      ? envUrl
      : `https://${envUrl}`
    : resolveFallbackGatewayOrigin()

  return `${normalizedOrigin.replace(/\/+$/, '')}/api`
}

type FetchJsonOptions = {
  method?: string
  headers?: HeadersInit
  body?: BodyInit | null
  timeoutMs?: number
  signal?: AbortSignal
}

export function buildAuthHeaders(headers?: HeadersInit): Headers {
  const mergedHeaders = new Headers(headers)
  const token = getAccessToken()
  if (token && !mergedHeaders.has('Authorization')) {
    mergedHeaders.set('Authorization', `Bearer ${token}`)
  }

  return mergedHeaders
}

function mergeSignals(signal?: AbortSignal, timeoutSignal?: AbortSignal) {
  if (!signal) return timeoutSignal
  if (!timeoutSignal) return signal

  const controller = new AbortController()

  const abort = () => controller.abort()

  if (signal.aborted || timeoutSignal.aborted) {
    controller.abort()
    return controller.signal
  }

  signal.addEventListener('abort', abort, { once: true })
  timeoutSignal.addEventListener('abort', abort, { once: true })

  return controller.signal
}

export async function fetchJson<T>(
  url: string,
  options: FetchJsonOptions = {},
): Promise<T> {
  const controller = new AbortController()
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs)

  const signal = mergeSignals(options.signal, controller.signal)

  try {
    const res = await fetch(url, {
      method: options.method,
      headers: buildAuthHeaders(options.headers),
      body: options.body,
      signal,
    })

    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      throw new Error(
        (body as { error?: string }).error ?? `HTTP ${res.status}`,
      )
    }

    return res.json() as Promise<T>
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new Error(`Request timed out after ${timeoutMs}ms`)
    }
    throw error
  } finally {
    clearTimeout(timeoutId)
  }
}
