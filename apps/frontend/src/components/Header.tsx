import { Link, useRouterState } from '@tanstack/react-router'
import { AnimatePresence, motion } from 'motion/react'
import {
  RiArrowRightSLine,
  RiCloseLine,
  RiLogoutBoxLine,
  RiMenuLine,
  RiMoonLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { BuildFooter, NavList } from '@/components/app-sidebar'
import {
  GatewayHealthIndicator,
  RealtimeIndicator,
} from '@/components/status-indicators'
import { Button } from '@/components/ui/button'
import { usePasswordAuth } from '@/features/auth/password-provider'
import { pageMetaForRouteId } from '@/lib/navigation'
import { useTheme } from '@/lib/theme'
import { cn } from '@/lib/utils'

/**
 * Page header used by every console page.
 *
 * - Title and one-line description come from the navigation config for the
 *   current route (override with `title` / `description`).
 * - `children` are page actions (search, filters, refresh) shown on the right.
 * - `subBar` is an optional full-width second row (filters); it is always shown
 *   on small screens and also on desktop when `subBarAlways` is set.
 * - The right edge always shows the realtime stream state, gateway health,
 *   theme toggle and (on mobile) the navigation drawer.
 */
export default function Header({
  children,
  subBar,
  subBarAlways = false,
  title,
  titleSuffix,
  description,
}: {
  children?: React.ReactNode
  subBar?: React.ReactNode
  /** Show `subBar` on all screen sizes (default: only below `sm`). */
  subBarAlways?: boolean
  title?: string
  /** Extra inline content after the title, e.g. an ID with a copy button. */
  titleSuffix?: React.ReactNode
  description?: React.ReactNode
}) {
  const [isOpen, setIsOpen] = useState(false)
  const openButtonRef = useRef<HTMLButtonElement>(null)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const shouldRestoreFocusRef = useRef(false)
  const { logout } = usePasswordAuth()
  const { theme, toggleTheme } = useTheme()
  const routeId = useRouterState({
    select: (state) => state.matches.at(-1)?.routeId as string | undefined,
  })
  const meta = pageMetaForRouteId(routeId)
  const pageTitle = title ?? meta?.title ?? 'Gateway Console'
  const pageDescription = description ?? meta?.description
  const Icon = meta?.icon

  const close = useCallback(() => {
    shouldRestoreFocusRef.current = true
    setIsOpen(false)
  }, [])

  useEffect(() => {
    if (!isOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [isOpen, close])

  useEffect(() => {
    if (isOpen) closeButtonRef.current?.focus()
  }, [isOpen])

  return (
    <>
      <header className="shrink-0 border-b border-border bg-background/90 text-foreground backdrop-blur">
        <div className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2">
          <button
            ref={openButtonRef}
            onClick={() => setIsOpen(true)}
            className="rounded-lg p-1.5 transition-colors hover:bg-muted lg:hidden"
            aria-label="Open navigation menu"
          >
            <RiMenuLine size={18} />
          </button>

          <div className="flex min-w-0 flex-1 basis-56 items-center gap-2">
            {Icon ? (
              <Icon className="hidden size-4 shrink-0 text-cyan-600 sm:block dark:text-cyan-400" />
            ) : null}
            <div className="min-w-0">
              <div className="flex min-w-0 items-center gap-1 text-sm">
                {meta?.parent ? (
                  <>
                    <Link
                      to={meta.parent.to}
                      className="shrink-0 text-muted-foreground hover:text-foreground hover:underline"
                    >
                      {meta.parent.label}
                    </Link>
                    <RiArrowRightSLine className="size-3.5 shrink-0 text-muted-foreground" />
                  </>
                ) : null}
                <h1 className="truncate font-semibold">{pageTitle}</h1>
                {titleSuffix ? (
                  <div className="ml-1 flex min-w-0 items-center">
                    {titleSuffix}
                  </div>
                ) : null}
              </div>
              {pageDescription ? (
                <p className="hidden truncate text-[11px] text-muted-foreground md:block">
                  {pageDescription}
                </p>
              ) : null}
            </div>
          </div>

          <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2 text-xs">
            {children}
            <RealtimeIndicator />
            <GatewayHealthIndicator />
            <Button
              size="icon"
              variant="ghost"
              className="size-7"
              onClick={toggleTheme}
              aria-label={
                theme === 'dark'
                  ? 'Switch to light mode'
                  : 'Switch to dark mode'
              }
              title={theme === 'dark' ? 'Light mode' : 'Dark mode'}
            >
              {theme === 'dark' ? (
                <RiSunLine className="size-3.5" />
              ) : (
                <RiMoonLine className="size-3.5" />
              )}
            </Button>
          </div>
        </div>
        {subBar ? (
          <div
            className={cn(
              'border-t border-border/60 px-4 py-2',
              !subBarAlways && 'sm:hidden',
            )}
          >
            {subBar}
          </div>
        ) : null}
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
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2, ease: 'easeOut' }}
              className="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm lg:hidden"
              onClick={close}
              aria-hidden="true"
            />
            <motion.aside
              role="dialog"
              aria-modal="true"
              aria-label="Navigation menu"
              initial={{ x: '-100%' }}
              animate={{ x: 0 }}
              exit={{ x: '-100%' }}
              transition={{ duration: 0.25, ease: 'easeInOut' }}
              className="fixed left-0 top-0 z-50 flex h-full w-72 flex-col border-r border-border bg-background text-foreground shadow-2xl lg:hidden"
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
              <div className="flex-1 overflow-y-auto p-3">
                <NavList onNavigate={close} />
              </div>
              <div className="space-y-2 border-t border-border px-4 py-3">
                <BuildFooter />
                <Button
                  size="sm"
                  variant="outline"
                  className="h-8 w-full justify-start gap-2 text-xs"
                  onClick={logout}
                >
                  <RiLogoutBoxLine className="size-3.5" />
                  Logout
                </Button>
              </div>
            </motion.aside>
          </>
        ) : null}
      </AnimatePresence>
    </>
  )
}
