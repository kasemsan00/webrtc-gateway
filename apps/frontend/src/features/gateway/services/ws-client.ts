export function createGatewayWebSocket(url: string) {
  return new WebSocket(url)
}

export function isWebSocketOpen(ws: WebSocket | null) {
  return ws?.readyState === WebSocket.OPEN
}

export function sendJson(ws: WebSocket | null, payload: unknown) {
  if (!isWebSocketOpen(ws)) return false
  ws.send(JSON.stringify(payload))
  return true
}
