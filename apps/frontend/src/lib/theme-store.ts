import { Store } from '@tanstack/store'

import { attachPersist, migrateLegacyPersisted } from './store-persist'

type Theme = 'light' | 'dark'

interface ThemeState {
  theme: Theme
}

const DEFAULT_THEME: Theme = 'dark'
const PERSIST_KEY = 'webrtc-sip-gateway-theme'
const LEGACY_PERSIST_KEY = 'k2-theme'
const PERSIST_VERSION = 1

export const themeStore = new Store<ThemeState>({
  theme: DEFAULT_THEME,
})

let initialized = false

function isTheme(value: unknown): value is Theme {
  return value === 'light' || value === 'dark'
}

export function initializeThemeStore() {
  if (typeof window === 'undefined' || initialized) return
  initialized = true

  migrateLegacyPersisted(PERSIST_KEY, LEGACY_PERSIST_KEY, PERSIST_VERSION, isTheme)

  attachPersist<ThemeState, Theme>(themeStore, {
    key: PERSIST_KEY,
    version: PERSIST_VERSION,
    debounceMs: 100,
    select: (state) => state.theme,
    merge: (persisted, current) => {
      const value = persisted as unknown
      if (isTheme(value)) {
        return { ...current, theme: value }
      }
      return current
    },
  })
}

export function toggleTheme() {
  themeStore.setState((state) => ({
    ...state,
    theme: state.theme === 'dark' ? 'light' : 'dark',
  }))
}

export function setTheme(t: Theme) {
  themeStore.setState((state) => ({
    ...state,
    theme: t,
  }))
}
