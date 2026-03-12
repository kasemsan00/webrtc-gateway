import type { Store } from '@tanstack/store'

interface PersistedEnvelope<TData> {
  version: number
  data: TData
}

export interface StorePersistOptions<TState, TPersistedData> {
  /** localStorage key */
  key: string
  /** Schema version. Bumping this discards stale data. */
  version?: number
  /** Pick the fields you want to persist from full state. */
  select: (state: TState) => TPersistedData
  /** Merge persisted data back into the current (initial) state. */
  merge: (persisted: TPersistedData, current: TState) => TState
  /** Debounce writes to localStorage (ms). Default 300. */
  debounceMs?: number
}

/**
 * Load persisted data from localStorage.
 * Returns `null` when the key is missing, corrupt, or the version doesn't match.
 */
export function loadPersisted<TData>(
  key: string,
  version: number,
): TData | null {
  if (typeof window === 'undefined') return null

  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return null

    const envelope = JSON.parse(raw) as unknown
    if (!envelope || typeof envelope !== 'object') {
      return null
    }

    const typedEnvelope = envelope as PersistedEnvelope<TData>
    if (typedEnvelope.version !== version) {
      return null
    }

    return typedEnvelope.data
  } catch {
    return null
  }
}

/**
 * Write persisted data to localStorage.
 */
function writePersisted<TData>(
  key: string,
  version: number,
  data: TData,
): void {
  if (typeof window === 'undefined') return

  try {
    const envelope: PersistedEnvelope<TData> = { version, data }
    window.localStorage.setItem(key, JSON.stringify(envelope))
  } catch {
    // Storage full or unavailable – silently ignore.
  }
}

/**
 * Clear persisted data for a given key.
 */
export function clearPersisted(key: string): void {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(key)
}

/**
 * Attach persistence to a TanStack Store.
 *
 * 1. Hydrates the store from localStorage (if data exists and version matches).
 * 2. Subscribes to store changes and writes the selected slice back (debounced).
 *
 * Returns an `unsubscribe` function to detach the listener.
 */
export function attachPersist<TState, TPersistedData>(
  store: Store<TState>,
  options: StorePersistOptions<TState, TPersistedData>,
): () => void {
  const { key, version = 1, select, merge, debounceMs = 300 } = options

  // --- Hydrate ---
  const saved = loadPersisted<TPersistedData>(key, version)
  if (saved !== null) {
    store.setState((current) => merge(saved, current))
  }

  // --- Subscribe & write (debounced) ---
  let timer: ReturnType<typeof setTimeout> | null = null
  let lastJson = ''

  const subscription = store.subscribe(() => {
    if (timer) clearTimeout(timer)

    timer = setTimeout(() => {
      timer = null
      const data = select(store.state)
      const json = JSON.stringify(data)

      // Skip write if nothing changed.
      if (json === lastJson) return
      lastJson = json

      writePersisted(key, version, data)
    }, debounceMs)
  })

  return () => {
    if (timer) clearTimeout(timer)

    const unsubscribeTarget = subscription as
      | { unsubscribe?: () => void }
      | (() => void)

    if (typeof unsubscribeTarget === 'function') {
      unsubscribeTarget()
      return
    }

    unsubscribeTarget.unsubscribe?.()
  }
}
