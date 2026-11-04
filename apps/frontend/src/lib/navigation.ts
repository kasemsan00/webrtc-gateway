import {
  RiAccountCircleLine,
  RiBarChartGroupedLine,
  RiBugLine,
  RiComputerLine,
  RiFileTextLine,
  RiHistoryLine,
  RiPulseLine,
  RiRouteLine,
  RiRouterLine,
  RiServerLine,
  RiSettings3Line,
} from '@remixicon/react'
import type { ComponentType } from 'react'

export type NavIcon = ComponentType<{
  className?: string
  size?: number | string
}>

export type NavPath =
  | '/'
  | '/active-sessions'
  | '/trunks'
  | '/ws-clients'
  | '/sessions'
  | '/logs'
  | '/client-diagnostics'
  | '/instances'
  | '/session-directory'
  | '/public-accounts'
  | '/settings'

export interface NavItem {
  to: NavPath
  label: string
  icon: NavIcon
  /** One line shown under the page title: what this page is for. */
  description: string
}

export interface NavGroup {
  label: string
  /** Short hint shown under the group label in the sidebar. */
  hint: string
  items: Array<NavItem>
}

export const NAV_GROUPS: Array<NavGroup> = [
  {
    label: 'Live',
    hint: 'What is happening right now',
    items: [
      {
        to: '/',
        label: 'Dashboard',
        icon: RiBarChartGroupedLine,
        description:
          'Gateway health, live load and call outcomes for the selected period.',
      },
      {
        to: '/active-sessions',
        label: 'Active Sessions',
        icon: RiPulseLine,
        description:
          'Calls in progress on this gateway. Hang up or send DTMF from here.',
      },
      {
        to: '/trunks',
        label: 'Trunks',
        icon: RiServerLine,
        description:
          'SIP trunks: registration state, last error, owning instance and live calls.',
      },
      {
        to: '/ws-clients',
        label: 'WS Clients',
        icon: RiRouterLine,
        description:
          'Apps and agents connected over WebSocket and the call each one is in.',
      },
    ],
  },
  {
    label: 'Investigate',
    hint: 'Debug a call or a client',
    items: [
      {
        to: '/sessions',
        label: 'Call Sessions',
        icon: RiHistoryLine,
        description:
          'Call history. Open a call for its SIP events, payloads, dialogs and media stats.',
      },
      {
        to: '/logs',
        label: 'Gateway Logs',
        icon: RiFileTextLine,
        description:
          'Tail the gateway log files. Search by session ID, Call-ID or any text.',
      },
      {
        to: '/client-diagnostics',
        label: 'Client Diagnostics',
        icon: RiBugLine,
        description:
          'Diagnostic events uploaded by mobile and desktop clients.',
      },
    ],
  },
  {
    label: 'Cluster',
    hint: 'Multi-instance routing',
    items: [
      {
        to: '/instances',
        label: 'Instances',
        icon: RiComputerLine,
        description:
          'Gateway instances sending heartbeats. Expired means the instance stopped reporting.',
      },
      {
        to: '/session-directory',
        label: 'Session Directory',
        icon: RiRouteLine,
        description:
          'Which instance owns each live session, used to route resume and control requests.',
      },
      {
        to: '/public-accounts',
        label: 'Public Accounts',
        icon: RiAccountCircleLine,
        description:
          'Shared SIP registrations created on demand for public (guest) callers.',
      },
    ],
  },
  {
    label: 'Config',
    hint: 'Read-only',
    items: [
      {
        to: '/settings',
        label: 'Configuration',
        icon: RiSettings3Line,
        description:
          'Effective gateway configuration (read-only). Secrets are redacted.',
      },
    ],
  },
]

export const NAV_ITEMS: Array<NavItem> = NAV_GROUPS.flatMap(
  (group) => group.items,
)

export interface PageMeta {
  title: string
  description: string
  icon?: NavIcon
  /** Parent page for breadcrumb-style "back" navigation. */
  parent?: NavItem
}

const SESSIONS_ITEM = NAV_ITEMS.find((item) => item.to === '/sessions')

/** Maps a TanStack route id (e.g. `/sessions/$sessionId`) to page metadata. */
export function pageMetaForRouteId(
  routeId: string | undefined,
): PageMeta | null {
  if (!routeId) return null
  if (routeId === '/sessions/$sessionId') {
    return {
      title: 'Session Detail',
      description:
        'Everything recorded for one call: outcome, SIP timeline, payloads, dialogs, RTCP stats and client diagnostics.',
      icon: SESSIONS_ITEM?.icon,
      parent: SESSIONS_ITEM,
    }
  }
  const normalized =
    routeId.length > 1 && routeId.endsWith('/') ? routeId.slice(0, -1) : routeId
  const item = NAV_ITEMS.find((entry) => entry.to === normalized)
  if (!item) return null
  return { title: item.label, description: item.description, icon: item.icon }
}

export const APP_TITLE = 'WebRTC-SIP Gateway Console'

/** Browser tab title for a page. */
export function documentTitle(page: string): string {
  return `${page} · ${APP_TITLE}`
}

/** `head` option for a file route so each tab shows its own page name. */
export function routeHead(page: string) {
  return () => ({ meta: [{ title: documentTitle(page) }] })
}
