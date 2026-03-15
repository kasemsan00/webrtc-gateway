export function createGatewayWebSocket(url: string) {
  return new WebSocket(url)
}

export function buildGatewayWebSocketUrl(
  url: string,
  accessToken?: string | null,
) {
  const token = accessToken?.trim()
  if (!token) return url

  try {
    const parsed = new URL(url)
    parsed.searchParams.set('access_token', token)
    return parsed.toString()
  } catch {
    const sep = url.includes('?') ? '&' : '?'
    return `${url}${sep}access_token=${encodeURIComponent(token)}`
  }
}

export function isWebSocketOpen(ws: WebSocket | null) {
  return ws?.readyState === WebSocket.OPEN
}

export function sendJson(ws: WebSocket | null, payload: unknown) {
  if (!isWebSocketOpen(ws)) return false
  ws.send(JSON.stringify(payload))
  return true
}
