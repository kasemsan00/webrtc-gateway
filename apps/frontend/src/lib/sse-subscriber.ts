import { buildAuthHeaders } from '@/lib/http-client'

export type SseEventHandler<T> = (event: T) => void

export type SubscribeAuthenticatedSseOptions<T> = {
  url: string
  eventName: string
  onEvent: SseEventHandler<T>
  onError?: (event: Event) => void
  headers?: HeadersInit
}

export function subscribeAuthenticatedSse<T>(
  options: SubscribeAuthenticatedSseOptions<T>,
): () => void {
  const controller = new AbortController()
  const decoder = new TextDecoder()
  let buffer = ''

  const handleChunk = (chunk: string) => {
    buffer += chunk.replace(/\r\n/g, '\n')

    for (;;) {
      const eventBoundary = buffer.indexOf('\n\n')
      if (eventBoundary === -1) break

      const rawEvent = buffer.slice(0, eventBoundary)
      buffer = buffer.slice(eventBoundary + 2)

      const lines = rawEvent.split(/\r?\n/)
      let eventName = 'message'
      const dataLines: Array<string> = []

      for (const line of lines) {
        if (line.startsWith('event:')) {
          eventName = line.slice('event:'.length).trim()
          continue
        }

        if (line.startsWith('data:')) {
          dataLines.push(line.slice('data:'.length).trim())
        }
      }

      if (eventName !== options.eventName || dataLines.length === 0) continue

      try {
        const parsed = JSON.parse(dataLines.join('\n')) as T
        options.onEvent(parsed)
      } catch {
        // Ignore malformed event payloads.
      }
    }
  }

  const start = async () => {
    try {
      const response = await fetch(options.url, {
        method: 'GET',
        headers: buildAuthHeaders({
          Accept: 'text/event-stream',
          ...options.headers,
        }),
        signal: controller.signal,
      })

      if (!response.ok || !response.body) {
        throw new Error(`Failed to subscribe SSE: HTTP ${response.status}`)
      }

      const reader = response.body.getReader()
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break

        handleChunk(decoder.decode(value, { stream: true }))
      }
    } catch {
      if (!controller.signal.aborted && options.onError) {
        options.onError(new Event('error'))
      }
    }
  }

  void start()

  return () => {
    controller.abort()
  }
}