import { Link } from '@tanstack/react-router'
import { AnimatePresence, motion } from 'motion/react'
import { toast } from 'sonner'

import {
  RiAccountCircleLine,
  RiCloseLine,
  RiComputerLine,
  RiHistoryLine,
  RiLogoutBoxLine,
  RiMenuLine,
  RiPhoneLine,
  RiPulseLine,
  RiRouteLine,
  RiServerLine,
} from '@remixicon/react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { useKeycloakAuth } from '@/features/auth/keycloak-provider'
import { Button } from '@/components/ui/button'

export default function Header({ children }: { children?: React.ReactNode }) {
  const [isOpen, setIsOpen] = useState(false)
  const [isLoggingOut, setIsLoggingOut] = useState(false)
  const openButtonRef = useRef<HTMLButtonElement>(null)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const shouldRestoreFocusRef = useRef(false)
  const { user, logout } = useKeycloakAuth()

  const userLabel = user?.displayName || 'Unknown user'
  const usernameLabel = user?.username || '-'

  const handleLogout = useCallback(async () => {
    if (isLoggingOut) return

    setIsLoggingOut(true)
    try {
      await logout()
    } catch (error) {
      toast.error('Logout failed', {
        description:
          error instanceof Error ? error.message : 'Please try again',
      })
      setIsLoggingOut(false)
    }
  }, [isLoggingOut, logout])

  const close = useCallback(() => {
    shouldRestoreFocusRef.current = true
    setIsOpen(false)
  }, [])

  // Close on Escape
  useEffect(() => {
    if (!isOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [isOpen, close])

  // Auto-focus close button when drawer opens
  useEffect(() => {
    if (isOpen) closeButtonRef.current?.focus()
  }, [isOpen])

  return (
    <>
      <header className="flex items-center gap-3 border-b border-border bg-background/90 px-4 py-2 text-foreground backdrop-blur">
        <button
          ref={openButtonRef}
          onClick={() => setIsOpen(true)}
          className="rounded-lg p-1.5 transition-colors hover:bg-muted"
          aria-label="Open navigation menu"
        >
          <RiMenuLine size={18} />
        </button>
        <Link to="/" className="flex items-center gap-2">
          <span className="bg-linear-to-r from-cyan-600 to-emerald-600 bg-clip-text text-sm font-bold text-transparent dark:from-cyan-400 dark:to-emerald-400">
            WebRTC Gateway
          </span>
        </Link>
        <div className="ml-auto flex items-center gap-2">
          {children}
          <div className="hidden min-w-0 items-center gap-2 rounded-md border border-border/60 bg-muted/40 px-2 py-1 md:flex">
            <RiAccountCircleLine className="size-4 text-muted-foreground" />
            <div className="min-w-0">
              <p className="truncate text-[11px] text-muted-foreground">
                {usernameLabel}
              </p>
            </div>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => {
              void handleLogout()
            }}
            disabled={isLoggingOut}
          >
            <RiLogoutBoxLine className="size-3.5" />
            <span className="hidden sm:inline">
              {isLoggingOut ? 'Logging out...' : 'Logout'}
            </span>
          </Button>
        </div>
      </header>

      <AnimatePresence
        onExitComplete={() => {
          if (shouldRestoreFocusRef.current) {
            openButtonRef.current?.focus()
            shouldRestoreFocusRef.current = false
          }
        }}
      >
        {isOpen ? (
          <>
            {/* Overlay backdrop */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2, ease: 'easeOut' }}
              className="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm"
              onClick={close}
              aria-hidden="true"
            />

            {/* Sidebar drawer */}
            <motion.aside
              role="dialog"
              aria-modal="true"
              aria-label="Navigation menu"
              initial={{ x: '-100%' }}
              animate={{ x: 0 }}
              exit={{ x: '-100%' }}
              transition={{ duration: 0.25, ease: 'easeInOut' }}
              className="fixed left-0 top-0 z-50 flex h-full w-72 flex-col border-r border-border bg-background text-foreground shadow-2xl"
            >
              <div className="flex items-center justify-between border-b border-border px-4 py-3">
                <span className="text-sm font-semibold tracking-wide text-muted-foreground">
                  Navigation
                </span>
                <button
                  ref={closeButtonRef}
                  onClick={close}
                  className="rounded-lg p-1.5 transition-colors hover:bg-muted"
                  aria-label="Close navigation menu"
                >
                  <RiCloseLine size={18} />
                </button>
              </div>

              <nav className="flex-1 overflow-y-auto p-3">
                <Link
                  to="/"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiPhoneLine size={16} />
                  <span className="font-medium">Gateway Console</span>
                </Link>
                <Link
                  to="/trunks"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiServerLine size={16} />
                  <span className="font-medium">Trunks</span>
                </Link>
                <Link
                  to="/sessions"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiHistoryLine size={16} />
                  <span className="font-medium">Call Sessions</span>
                </Link>
                <Link
                  to="/active-sessions"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiPulseLine size={16} />
                  <span className="font-medium">Active Sessions</span>
                </Link>
                <Link
                  to="/public-accounts"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiAccountCircleLine size={16} />
                  <span className="font-medium">Public Accounts</span>
                </Link>
                <Link
                  to="/instances"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiComputerLine size={16} />
                  <span className="font-medium">Instances</span>
                </Link>
                <Link
                  to="/session-directory"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiRouteLine size={16} />
                  <span className="font-medium">Session Directory</span>
                </Link>
              </nav>

              <div className="border-t border-border px-4 py-3">
                <div className="mb-3 rounded-md border border-border/60 bg-muted/40 p-2">
                  <div className="mb-1 flex items-center gap-2">
                    <RiAccountCircleLine className="size-4 text-muted-foreground" />
                    <p className="truncate text-sm font-medium">{userLabel}</p>
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  className="h-8 w-full justify-start gap-2 text-xs"
                  onClick={() => {
                    void handleLogout()
                  }}
                  disabled={isLoggingOut}
                >
                  <RiLogoutBoxLine className="size-3.5" />
                  {isLoggingOut ? 'Logging out...' : 'Logout'}
                </Button>
                <p className="mt-3 text-xs text-muted-foreground/60">
                  WebRTC Gateway
                </p>
              </div>
            </motion.aside>
          </>
        ) : null}
      </AnimatePresence>
    </>
  )
}
