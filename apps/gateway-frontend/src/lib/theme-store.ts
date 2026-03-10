import { Store } from '@tanstack/store'

import { attachPersist } from './store-persist'

type Theme = 'light' | 'dark'

interface ThemeState {
  theme: Theme
}

const DEFAULT_THEME: Theme = 'dark'
const PERSIST_KEY = 'k2-theme'
const PERSIST_VERSION = 1

export const themeStore = new Store<ThemeState>({
  theme: DEFAULT_THEME,
})

let initialized = false

export function initializeThemeStore() {
  if (typeof window === 'undefined' || initialized) return
  initialized = true

  attachPersist<ThemeState, Theme>(themeStore, {
    key: PERSIST_KEY,
    version: PERSIST_VERSION,
    debounceMs: 100,
    select: (state) => state.theme,
    merge: (persisted, current) => {
      const value = persisted as unknown
      if (value === 'light' || value === 'dark') {
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
