import type { VideoConfig } from './types'
import { readAppEnvValue } from '@/lib/runtime-env'

const isBrowser = () => typeof window !== 'undefined'

export function normalizeGatewayWssUrl(rawUrl: string) {
  const url = rawUrl.trim()
  if (!url) return url

  try {
    const parsed = new URL(url)
    if (parsed.pathname === '' || parsed.pathname === '/') {
      parsed.pathname = '/ws'
    } else if (!parsed.pathname.endsWith('/ws')) {
      parsed.pathname = `${parsed.pathname.replace(/\/+$/, '')}/ws`
    }
    return parsed.toString()
  } catch {
    if (url.endsWith('/ws')) return url
    return `${url.replace(/\/+$/, '')}/ws`
  }
}

function resolveDefaultWssUrl() {
  const envUrl = readAppEnvValue('VITE_GATEWAY_URL')
  if (envUrl) return normalizeGatewayWssUrl(`wss://${envUrl}`)

  if (!isBrowser()) {
    return 'ws://localhost:8000/ws'
  }

  if (window.location.hostname === 'k2-gateway.kasemsan.com') {
    return 'wss://k2-gateway.kasemsan.com/ws'
  }

  const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${protocol}://${window.location.hostname}:8000/ws`
}

export const gatewayRuntimeConfig = {
  wssUrl: resolveDefaultWssUrl(),
  turn: {
    url: readAppEnvValue('VITE_TURN_URL') ?? '',
    username: readAppEnvValue('VITE_TURN_USERNAME') ?? '',
    credential: readAppEnvValue('VITE_TURN_CREDENTIAL') ?? '',
  },
}

export const defaultVideoConfig: VideoConfig = {
  maxBitrate: 3000,
  maxFramerate: 30,
  width: 1280,
  height: 720,
  useConstrainedBaseline: false,
}
