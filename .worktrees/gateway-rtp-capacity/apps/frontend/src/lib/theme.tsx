import { useStore } from '@tanstack/react-store'
import { useEffect } from 'react'

import {
  initializeThemeStore,
  setTheme,
  themeStore,
  toggleTheme,
} from './theme-store'

export function useTheme() {
  const { theme } = useStore(themeStore, (state) => state)

  useEffect(() => {
    initializeThemeStore()
  }, [])

  useEffect(() => {
    const root = document.documentElement
    if (theme === 'dark') {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }
  }, [theme])

  return { theme, toggleTheme, setTheme }
}
