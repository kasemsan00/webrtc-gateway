import { useEffect, useState } from 'react'

/**
 * Current time in ms, re-rendering every `intervalMs` while the tab is
 * visible. Used for ticking call timers and "x ago" labels.
 */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') setNow(Date.now())
    }, intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])

  return now
}
