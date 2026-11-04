import { readAppEnvValue } from './runtime-env'
import { getAccessToken } from '@/features/auth/token-store'

const DEFAULT_TIMEOUT_MS = 15_000

const DEFAULT_GATEWAY_PORT = '8000'

const isBrowser = () => typeof window !== 'undefined'

function resolveFallbackGatewayOrigin(): string {
  if (!isBrowser()) {
    return `http://localhost:${DEFAULT_GATEWAY_PORT}`
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

/** Error thrown for non-2xx responses; keeps status and path for debugging. */
export class HttpError extends Error {
  readonly status: number
  readonly path: string

  constructor(message: string, status: number, path: string) {
    super(message)
    this.name = 'HttpError'
    this.status = status
    this.path = path
  }
}

function pathOf(url: string): string {
  try {
    const parsed = new URL(url)
    return parsed.pathname
  } catch {
    return url.split('?')[0] ?? url
  }
}

const STATUS_HINT: Partial<Record<number, string>> = {
  401: 'not authorised: sign out and sign in again, or check FRONTEND_PASSWORD on the gateway',
  403: 'forbidden',
  404: 'not found',
  502: 'bad gateway: the reverse proxy cannot reach the gateway',
  503: 'service unavailable',
  504: 'gateway timeout',
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
      const path = pathOf(url)
      const detail =
        (body as { error?: string }).error ?? STATUS_HINT[res.status] ?? ''
      throw new HttpError(
        `${detail ? `${detail} ` : ''}(HTTP ${res.status} ${options.method ?? 'GET'} ${path})`,
        res.status,
        path,
      )
    }

    return res.json() as Promise<T>
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new Error(
        `Request timed out after ${timeoutMs}ms (${options.method ?? 'GET'} ${pathOf(url)})`,
      )
    }
    if (error instanceof TypeError) {
      // fetch() network failure: DNS, CORS, TLS or the gateway is down.
      throw new Error(
        `Cannot reach the gateway (${options.method ?? 'GET'} ${pathOf(url)}): ${error.message}`,
      )
    }
    throw error
  } finally {
    clearTimeout(timeoutId)
  }
}
