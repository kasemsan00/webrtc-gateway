import { Store } from '@tanstack/store'

import { attachPersist } from '@/lib/store-persist'

type ViewMode = 'card' | 'table'

interface TrunkPrefsState {
  viewMode: ViewMode
}

const PERSIST_KEY = 'k2_trunk_prefs'
const PERSIST_VERSION = 1

export const trunkPrefsStore = new Store<TrunkPrefsState>({
  viewMode: 'card',
})

let initialized = false

export function initializeTrunkPrefsStore() {
  if (typeof window === 'undefined' || initialized) return
  initialized = true

  attachPersist<TrunkPrefsState, ViewMode>(trunkPrefsStore, {
    key: PERSIST_KEY,
    version: PERSIST_VERSION,
    debounceMs: 200,
    select: (state) => state.viewMode,
    merge: (persisted, current) => {
      const value = persisted as unknown
      if (value === 'card' || value === 'table') {
        return { ...current, viewMode: value }
      }
      return current
    },
  })
}

export function setViewMode(mode: ViewMode) {
  trunkPrefsStore.setState((state) => ({
    ...state,
    viewMode: mode,
  }))
}
