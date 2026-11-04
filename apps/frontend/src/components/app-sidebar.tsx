import { Link } from '@tanstack/react-router'
import { RiLogoutBoxLine } from '@remixicon/react'

import { Button } from '@/components/ui/button'
import { usePasswordAuth } from '@/features/auth/password-provider'
import { useGatewayStatus } from '@/features/gateway-status/gateway-status-store'
import { APP_TITLE, NAV_GROUPS } from '@/lib/navigation'
import { cn } from '@/lib/utils'

const ITEM_CLASS =
  'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground'
const ACTIVE_CLASS =
  'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm bg-cyan-600/10 font-medium text-cyan-700 dark:bg-cyan-500/15 dark:text-cyan-300'

/** Grouped navigation shared by the desktop sidebar and the mobile drawer. */
export function NavList({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="space-y-4" aria-label="Main navigation">
      {NAV_GROUPS.map((group) => (
        <div key={group.label}>
          <div className="px-2.5 pb-1">
            <p className="text-[11px] font-semibold uppercase tracking-wider text-foreground/70">
              {group.label}
            </p>
            <p className="text-[10px] text-muted-foreground/70">{group.hint}</p>
          </div>
          <ul className="space-y-0.5">
            {group.items.map((item) => {
              const Icon = item.icon
              return (
                <li key={item.to}>
                  <Link
                    to={item.to}
                    onClick={onNavigate}
                    className={ITEM_CLASS}
                    activeProps={{ className: ACTIVE_CLASS }}
                    activeOptions={{ exact: item.to === '/' }}
                    title={item.description}
                  >
                    <Icon className="size-4 shrink-0" />
                    <span className="truncate">{item.label}</span>
                  </Link>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}

export function BuildFooter() {
  const { build, dashboard } = useGatewayStatus()
  return (
    <div className="space-y-0.5 text-[10px] leading-4 text-muted-foreground">
      <p className="font-medium text-foreground/70">{APP_TITLE}</p>
      <p>
        Gateway{' '}
        <span className="font-mono">
          {build ? build.version : '…'}
          {build?.dirty ? '+dirty' : ''}
        </span>
        {build?.commit ? (
          <span className="font-mono"> · {build.commit.slice(0, 7)}</span>
        ) : null}
      </p>
      {dashboard?.instanceId ? (
        <p className="truncate" title={dashboard.instanceId}>
          Instance <span className="font-mono">{dashboard.instanceId}</span>
        </p>
      ) : null}
    </div>
  )
}

export function AppSidebar({ className }: { className?: string }) {
  const { logout } = usePasswordAuth()
  return (
    <aside
      className={cn(
        'w-56 shrink-0 flex-col border-r border-border bg-muted/20',
        className,
      )}
    >
      <Link
        to="/"
        className="box-content flex h-14 shrink-0 items-center border-b border-border px-4"
      >
        <span className="bg-linear-to-r from-cyan-600 to-emerald-600 bg-clip-text text-sm font-bold text-transparent dark:from-cyan-400 dark:to-emerald-400">
          Gateway Console
        </span>
      </Link>
      <div className="min-h-0 flex-1 overflow-y-auto p-2.5">
        <NavList />
      </div>
      <div className="space-y-2 border-t border-border p-3">
        <BuildFooter />
        <Button
          size="sm"
          variant="outline"
          className="h-7 w-full justify-start gap-2 text-xs"
          onClick={logout}
        >
          <RiLogoutBoxLine className="size-3.5" />
          Logout
        </Button>
      </div>
    </aside>
  )
}
