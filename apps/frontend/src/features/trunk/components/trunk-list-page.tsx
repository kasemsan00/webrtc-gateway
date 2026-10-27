import {
  RiAddLine,
  RiLayoutColumnLine,
  RiListCheck,
  RiLoader4Line,
  RiMoonLine,
  RiMore2Fill,
  RiNodeTree,
  RiRefreshLine,
  RiServerLine,
  RiSunLine,
} from '@remixicon/react'
import { debounce } from '@tanstack/pacer'
import { useStore } from '@tanstack/react-store'
import { useSearch } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import type {
  ColumnDef,
  OnChangeFn,
  VisibilityState,
} from '@tanstack/react-table'

import type { LiveFilter } from '@/features/trunk/live-board'
import type {
  CreateTrunkPayload,
  TestIncomingPushStyle,
  Trunk,
  TrunkListParams,
  UpdateTrunkPayload,
} from '@/features/trunk/types'
import { normalizeTrunkUid } from '@/features/trunk/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ServerPaginationControls } from '@/components/ui/server-pagination-controls'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { formatThaiDateTime } from '@/lib/date-time'
import { cn } from '@/lib/utils'
import { useTheme } from '@/lib/theme'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import {
  createTrunk,
  fetchAllTrunks,
  fetchTrunks,
  refreshTrunks,
  registerTrunk,
  restoreTrunk,
  softDeleteTrunk,
  subscribeTrunkEvents,
  testIncomingPush,
  unregisterTrunk,
  updateTrunk,
} from '@/features/trunk/services/trunk-api'
import {
  initializeTrunkPrefsStore,
  setColumnVisibility as setPersistedColumnVisibility,
  setViewMode as setPersistedViewMode,
  toggleColumnVisibility,
  trunkPrefsStore,
} from '@/features/trunk/store/trunk-prefs-store'
import {
  ActiveCallsDisplay,
  isTrunkOnCall,
  PageActiveCallsChip,
  trunkTableRowClassName,
} from '@/features/trunk/components/trunk-active-visuals'
import { TrunkLiveBoard } from '@/features/trunk/components/trunk-live-board'
import { LiveBoardErrorBoundary } from '@/features/trunk/components/live-board-error-boundary'
import { normalizeTrunks } from '@/features/trunk/live-normalize'
import { useTrunkLiveData } from '@/features/trunk/use-trunk-live-data'

const DEFAULT_PAGE_SIZE = 20

const TRUNK_TABLE_COLUMN_TOGGLES = [
  { id: 'id', label: 'ID' },
  { id: 'name', label: 'Name', locked: true },
  { id: 'inUseBy', label: 'In Use By' },
  { id: 'register', label: 'Register' },
  { id: 'pushContact', label: 'Push Contact' },
  { id: 'status', label: 'Status' },
  { id: 'domain', label: 'Domain' },
  { id: 'port', label: 'Port' },
  { id: 'username', label: 'Username' },
  { id: 'transport', label: 'Transport' },
  { id: 'destination', label: 'Destination' },
  { id: 'calls', label: 'Calls' },
  { id: 'lastRegisteredAt', label: 'Last Registered' },
  { id: 'lastUnregisteredAt', label: 'Last Unregistered' },
  { id: 'lastOnline', label: 'Last Online' },
  { id: 'uid', label: 'UID' },
] as const

function isColumnVisible(columnVisibility: VisibilityState, columnId: string) {
  return columnVisibility[columnId] !== false
}

type TrunkSortMode =
  | 'activeCallsDesc'
  | 'activeCallsAsc'
  | 'idAsc'
  | 'idDesc'
  | 'nameAsc'
  | 'nameDesc'

function formatUid(publicId?: string) {
  return publicId?.trim() || '-'
}

function formatDestinations(destinations?: Array<string>) {
  if (!destinations || destinations.length === 0) return '-'
  return destinations
    .map((destination) => destination.trim())
    .filter((destination) => destination.length > 0)
    .join(', ')
}

function formatInUseBy(trunk: Pick<Trunk, 'in_use_by' | 'inUseBy'>) {
  const value = trunk.in_use_by ?? trunk.inUseBy
  return <Badge variant="success">{value ? value : 'Not in use'}</Badge>
}

export function isPushContactReady(trunk: Trunk) {
  return Boolean(
    trunk.pushContactReady &&
    trunk.pnAppId?.trim() &&
    trunk.pnType?.trim() &&
    trunk.pnTokenMasked?.trim(),
  )
}

export function canTestIncomingPush(trunk: Trunk) {
  return Boolean(
    trunk.fcmTokenReady || trunk.notifyUserBound || isPushContactReady(trunk),
  )
}

/** Secondary line under Push Contact badge (SIP PN vs FCM/notify paths). */
export function getPushContactDetailText(trunk: Trunk): string {
  if (isPushContactReady(trunk)) {
    return `${trunk.pnType}; ${trunk.pnTokenMasked}`
  }
  const via: string[] = []
  if (trunk.fcmTokenReady) via.push('FCM')
  if (trunk.notifyUserBound) via.push('notify user')
  if (via.length > 0) {
    return `Uses ${via.join(', ')} (no SIP PN contact)`
  }
  return 'No SIP PN contact'
}

function PushContactBadge({ trunk }: { trunk: Trunk }) {
  const ready = isPushContactReady(trunk)
  return (
    <Badge variant={ready ? 'success' : 'secondary'} className="text-[10px]">
      {ready ? 'Ready' : 'Not Ready'}
    </Badge>
  )
}

function PushContactDetails({
  trunk,
  compact,
}: {
  trunk: Trunk
  compact?: boolean
}) {
  const text = getPushContactDetailText(trunk)
  const ready = isPushContactReady(trunk)
  return (
    <span
      className={cn(
        'block text-[10px] leading-snug text-muted-foreground',
        ready && 'font-mono',
        compact && 'min-h-8 line-clamp-2',
      )}
      title={text}
    >
      {text}
    </span>
  )
}

function trunkIdentityText(trunk?: Trunk | null) {
  if (!trunk) return '-'
  return `${trunk.name} (#${trunk.id}, uid: ${formatUid(normalizeTrunkUid(trunk))})`
}

function formatRegisterErrorMessage(error: unknown) {
  const baseMessage =
    error instanceof Error ? error.message : 'Failed to register trunk'
  const normalized = baseMessage.toLowerCase()
  if (
    normalized.includes('transport=tcp') &&
    normalized.includes('i/o timeout')
  ) {
    return `${baseMessage} (TCP timeout: please edit trunk transport to UDP or verify firewall/NAT allows outbound TCP 5060)`
  }
  return baseMessage
}

export function isRegisterActionDisabled(trunk: Trunk) {
  return trunk.isRegistered || !trunk.enabled
}

export function getTrunkStatusLabel(enabled: boolean) {
  return enabled ? 'Enabled' : 'Deleted'
}

export function getTrunkLifecycleActionLabel(enabled: boolean) {
  return enabled ? 'Disable' : 'Restore'
}

type TrunkEditForm = {
  name: string
  domain: string
  port: string
  username: string
  password: string
  transport: 'tcp' | 'udp'
  enabled: boolean
  isDefault: boolean
}

type TrunkCreateForm = {
  name: string
  domain: string
  port: string
  username: string
  password: string
  transport: 'tcp' | 'udp'
  enabled: boolean
  isDefault: boolean
}

function newCreateForm(): TrunkCreateForm {
  return {
    name: '',
    domain: '',
    port: '5060',
    username: '',
    password: '',
    transport: 'udp',
    enabled: true,
    isDefault: false,
  }
}

function createEditForm(trunk: Trunk): TrunkEditForm {
  return {
    name: trunk.name,
    domain: trunk.domain,
    port: String(trunk.port),
    username: trunk.username,
    password: '',
    transport: trunk.transport === 'udp' ? 'udp' : 'tcp',
    enabled: trunk.enabled,
    isDefault: trunk.isDefault,
  }
}

export function TrunkListPage() {
  const { theme, toggleTheme } = useTheme()
  const routeSearch = useSearch({ from: '/trunks' })
  const [trunks, setTrunks] = useState<Array<Trunk>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [search, setSearch] = useState(routeSearch.search ?? '')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [sortMode, setSortMode] = useState<TrunkSortMode>('activeCallsDesc')
  const { viewMode, columnVisibility } = useStore(
    trunkPrefsStore,
    (state) => state,
  )

  useEffect(() => {
    initializeTrunkPrefsStore()
  }, [])

  const handleColumnVisibilityChange = useCallback<OnChangeFn<VisibilityState>>(
    (updater) => {
      const current = trunkPrefsStore.state.columnVisibility
      const next = typeof updater === 'function' ? updater(current) : updater
      setPersistedColumnVisibility(next)
    },
    [],
  )
  const [registerTarget, setRegisterTarget] = useState<Trunk | null>(null)
  const [registering, setRegistering] = useState(false)
  const [unregisterTarget, setUnregisterTarget] = useState<Trunk | null>(null)
  const [unregistering, setUnregistering] = useState(false)
  const [testPushTarget, setTestPushTarget] = useState<Trunk | null>(null)
  const [testPushStyle, setTestPushStyle] =
    useState<TestIncomingPushStyle>('data')
  const [testingPush, setTestingPush] = useState(false)
  const [softDeleteTarget, setSoftDeleteTarget] = useState<Trunk | null>(null)
  const [softDeleting, setSoftDeleting] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState<Trunk | null>(null)
  const [restoring, setRestoring] = useState(false)
  const [editTarget, setEditTarget] = useState<Trunk | null>(null)
  const [editForm, setEditForm] = useState<TrunkEditForm | null>(null)
  const [savingEdit, setSavingEdit] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<TrunkCreateForm>(newCreateForm)
  const [savingCreate, setSavingCreate] = useState(false)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const liveData = useTrunkLiveData()
  const [liveFilter, setLiveFilter] = useState<LiveFilter>('all')
  const liveWarning = liveData.sessionsError
    ? `Live calls unavailable: ${liveData.sessionsError}`
    : liveData.clientsError
      ? `Connected apps unavailable: ${liveData.clientsError}`
      : null

  const { pageActiveCalls, pageTrunksOnCall } = useMemo(() => {
    let calls = 0
    let onCall = 0
    for (const trunk of trunks) {
      if (isTrunkOnCall(trunk)) {
        onCall += 1
        calls += trunk.activeCallCount
      }
    }
    return { pageActiveCalls: calls, pageTrunksOnCall: onCall }
  }, [trunks])

  // Map sortMode to backend sort parameters
  const getSortParams = useCallback((): {
    sortBy: string
    sortDir: 'asc' | 'desc'
  } => {
    switch (sortMode) {
      case 'activeCallsDesc':
        return { sortBy: 'id', sortDir: 'desc' } // Note: activeCallCount requires special handling
      case 'activeCallsAsc':
        return { sortBy: 'id', sortDir: 'asc' } // Note: activeCallCount requires special handling
      case 'idDesc':
        return { sortBy: 'id', sortDir: 'desc' }
      case 'nameAsc':
        return { sortBy: 'name', sortDir: 'asc' }
      case 'nameDesc':
        return { sortBy: 'name', sortDir: 'desc' }
      case 'idAsc':
      default:
        return { sortBy: 'id', sortDir: 'asc' }
    }
  }, [sortMode])

  const searchRef = useRef(search)
  searchRef.current = search

  const load = useCallback(
    async (
      params?: Partial<TrunkListParams>,
      options?: { silent?: boolean },
    ) => {
      if (!options?.silent) {
        setLoading(true)
      }
      setError(null)
      try {
        const sortParams = getSortParams()
        const filters = {
          search: (params?.search ?? searchRef.current) || undefined,
          trunkId: params?.trunkId ?? routeSearch.trunkId,
          trunkPublicId: params?.trunkPublicId ?? routeSearch.trunkPublicId,
          sortBy: sortParams.sortBy,
          sortDir: sortParams.sortDir,
        }
        // The live board shows every trunk; only the table is paginated.
        const res =
          viewMode === 'card'
            ? await fetchAllTrunks(filters)
            : await fetchTrunks({
                ...filters,
                page: params?.page ?? page,
                pageSize: params?.pageSize ?? pageSize,
              })
        // Normalise once here: the API omits empty fields and may send nulls.
        let items = normalizeTrunks(res.items)
        if (sortMode === 'activeCallsDesc' || sortMode === 'activeCallsAsc') {
          items = [...items].sort((left, right) => {
            const diff = left.activeCallCount - right.activeCallCount
            return sortMode === 'activeCallsDesc' ? -diff : diff
          })
        }
        setTrunks(items)
        setTotal(typeof res.total === 'number' ? res.total : items.length)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch trunks')
      } finally {
        if (!options?.silent) {
          setLoading(false)
        }
      }
    },
    [
      page,
      pageSize,
      getSortParams,
      sortMode,
      viewMode,
      routeSearch.trunkId,
      routeSearch.trunkPublicId,
    ],
  )

  useEffect(() => {
    void load()
  }, [load])

  const loadRef = useRef(load)
  loadRef.current = load

  const handleSilentReload = useCallback(() => {
    void loadRef.current(undefined, { silent: true })
  }, [])

  useVisibilityRealtimeReload({
    subscribe: subscribeTrunkEvents,
    onReload: handleSilentReload,
    reloadDebounceMs: 500,
    pollIntervalMs: 10_000,
  })

  const debouncedSearch = useMemo(
    () =>
      debounce(
        (value: string) => {
          setPage(1)
          void loadRef.current({ page: 1, search: value })
        },
        { wait: 300 },
      ),
    [],
  )

  const handleRefresh = useCallback(async () => {
    setRefreshing(true)
    try {
      await refreshTrunks()
      await load()
      toast.success('Refresh completed', {
        description: 'Trunk list reloaded from the database',
      })
    } catch (err) {
      toast.error('Refresh failed', {
        description:
          err instanceof Error ? err.message : 'Failed to refresh trunks',
      })
    } finally {
      setRefreshing(false)
    }
  }, [load])

  const handlePageChange = useCallback((nextPage: number) => {
    setPage(nextPage)
  }, [])

  const handlePageSizeChange = useCallback((nextPageSize: number) => {
    setPageSize(nextPageSize)
    setPage(1)
    void loadRef.current({ page: 1, pageSize: nextPageSize })
  }, [])

  const openRegisterModal = useCallback((trunk: Trunk) => {
    setRegisterTarget(trunk)
  }, [])

  const confirmRegister = useCallback(async () => {
    if (!registerTarget) return
    setRegistering(true)
    try {
      await registerTrunk(registerTarget.id)
      setRegisterTarget(null)
      await load()
      toast.success('Register success', {
        description: trunkIdentityText(registerTarget),
      })
    } catch (err) {
      toast.error('Register failed', {
        description: formatRegisterErrorMessage(err),
      })
    } finally {
      setRegistering(false)
    }
  }, [registerTarget, load])

  const openUnregisterModal = useCallback((trunk: Trunk) => {
    setUnregisterTarget(trunk)
  }, [])

  const openTestPushModal = useCallback((trunk: Trunk) => {
    setTestPushStyle('data')
    setTestPushTarget(trunk)
  }, [])

  const confirmTestPush = useCallback(async () => {
    if (!testPushTarget) return
    setTestingPush(true)
    try {
      const result = await testIncomingPush(testPushTarget.id, testPushStyle)
      setTestPushTarget(null)
      const channels = result.channels?.filter(Boolean).join(', ') || 'unknown'
      const style = result.style || testPushStyle
      toast.success('Test push sent', {
        description: `${trunkIdentityText(testPushTarget)} via ${channels} (${style})`,
      })
    } catch (err) {
      toast.error('Test push failed', {
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    } finally {
      setTestingPush(false)
    }
  }, [testPushStyle, testPushTarget])

  const confirmUnregister = useCallback(async () => {
    if (!unregisterTarget) return
    setUnregistering(true)
    try {
      await unregisterTrunk(unregisterTarget.id)
      setUnregisterTarget(null)
      await load()
      toast.success('Unregister success', {
        description: trunkIdentityText(unregisterTarget),
      })
    } catch (err) {
      toast.error('Unregister failed', {
        description:
          err instanceof Error ? err.message : 'Failed to unregister trunk',
      })
    } finally {
      setUnregistering(false)
    }
  }, [unregisterTarget, load])

  const openSoftDeleteModal = useCallback((trunk: Trunk) => {
    setSoftDeleteTarget(trunk)
  }, [])

  const openRestoreModal = useCallback((trunk: Trunk) => {
    setRestoreTarget(trunk)
  }, [])

  const openEditModal = useCallback((trunk: Trunk) => {
    setEditTarget(trunk)
    setEditForm(createEditForm(trunk))
  }, [])

  const closeEditModal = useCallback(() => {
    setEditTarget(null)
    setEditForm(null)
  }, [])

  const confirmEdit = useCallback(async () => {
    if (!editTarget || !editForm) return

    const trimmedName = editForm.name.trim()
    const trimmedDomain = editForm.domain.trim()
    const trimmedUsername = editForm.username.trim()
    if (!trimmedName || !trimmedDomain || !trimmedUsername) {
      toast.error('Validation error', {
        description: 'name, domain, and username are required',
      })
      return
    }

    const port = Number(editForm.port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      toast.error('Validation error', {
        description: 'port must be between 1 and 65535',
      })
      return
    }

    const payload: UpdateTrunkPayload = {
      name: trimmedName,
      domain: trimmedDomain,
      port,
      username: trimmedUsername,
      transport: editForm.transport,
      enabled: editForm.enabled,
      isDefault: editForm.isDefault,
    }

    const nextPassword = editForm.password.trim()
    if (nextPassword) {
      payload.password = nextPassword
    }

    setSavingEdit(true)
    setError(null)
    try {
      await updateTrunk(editTarget.id, payload)
      closeEditModal()
      await load()
      toast.success('Update success', {
        description: trunkIdentityText(editTarget),
      })
    } catch (err) {
      toast.error('Update failed', {
        description:
          err instanceof Error ? err.message : 'Failed to update trunk',
      })
    } finally {
      setSavingEdit(false)
    }
  }, [closeEditModal, editForm, editTarget, load])

  const confirmSoftDelete = useCallback(async () => {
    if (!softDeleteTarget) return
    setSoftDeleting(true)
    try {
      await softDeleteTrunk(softDeleteTarget.id)
      setSoftDeleteTarget(null)
      await load()
      toast.success('Disable success', {
        description: trunkIdentityText(softDeleteTarget),
      })
    } catch (err) {
      toast.error('Disable failed', {
        description:
          err instanceof Error ? err.message : 'Failed to disable trunk',
      })
    } finally {
      setSoftDeleting(false)
    }
  }, [softDeleteTarget, load])

  const confirmRestore = useCallback(async () => {
    if (!restoreTarget) return
    setRestoring(true)
    try {
      await restoreTrunk(restoreTarget.id)
      setRestoreTarget(null)
      await load()
      toast.success('Restore success', {
        description: trunkIdentityText(restoreTarget),
      })
    } catch (err) {
      toast.error('Restore failed', {
        description:
          err instanceof Error ? err.message : 'Failed to restore trunk',
      })
    } finally {
      setRestoring(false)
    }
  }, [restoreTarget, load])

  const confirmCreate = useCallback(async () => {
    const trimmedName = createForm.name.trim()
    const trimmedDomain = createForm.domain.trim()
    const trimmedUsername = createForm.username.trim()
    const trimmedPassword = createForm.password.trim()
    if (!trimmedName || !trimmedDomain || !trimmedUsername) {
      toast.error('Validation error', {
        description: 'name, domain, and username are required',
      })
      return
    }
    if (!trimmedPassword) {
      toast.error('Validation error', {
        description: 'password is required',
      })
      return
    }

    const port = Number(createForm.port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      toast.error('Validation error', {
        description: 'port must be between 1 and 65535',
      })
      return
    }

    const payload: CreateTrunkPayload = {
      name: trimmedName,
      domain: trimmedDomain,
      port,
      username: trimmedUsername,
      password: trimmedPassword,
      transport: createForm.transport,
      enabled: createForm.enabled,
      isDefault: createForm.isDefault,
    }

    setSavingCreate(true)
    setError(null)
    try {
      await createTrunk(payload)
      setCreateOpen(false)
      setCreateForm(newCreateForm())
      await load()
      toast.success('Create success', {
        description: `Created trunk ${trimmedName}`,
      })
    } catch (err) {
      toast.error('Create failed', {
        description:
          err instanceof Error ? err.message : 'Failed to create trunk',
      })
    } finally {
      setSavingCreate(false)
    }
  }, [createForm, load])

  const renderSearch = (className: string) => (
    <Input
      className={className}
      placeholder="Search username or name..."
      aria-label="Search trunks"
      value={search}
      onChange={(e) => {
        setSearch(e.target.value)
        debouncedSearch(e.target.value)
      }}
    />
  )
  const sortSelect =
    viewMode === 'table' ? (
      <Select
        value={sortMode}
        onValueChange={(value) => setSortMode(value as TrunkSortMode)}
      >
        <SelectTrigger
          className="h-7 px-2 text-xs"
          size="sm"
          aria-label="Sort trunks"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="activeCallsDesc">
            Active Calls (High-Low)
          </SelectItem>
          <SelectItem value="activeCallsAsc">
            Active Calls (Low-High)
          </SelectItem>
          <SelectItem value="idAsc">ID (Low-High)</SelectItem>
          <SelectItem value="idDesc">ID (High-Low)</SelectItem>
          <SelectItem value="nameAsc">Name (A-Z)</SelectItem>
          <SelectItem value="nameDesc">Name (Z-A)</SelectItem>
        </SelectContent>
      </Select>
    ) : null
  const viewToggle = (
    <div
      className="flex items-center gap-0.5 rounded-md border p-0.5"
      role="group"
      aria-label="View mode"
    >
      <Button
        size="sm"
        variant={viewMode === 'card' ? 'secondary' : 'ghost'}
        className="h-6 gap-1 px-2 text-xs"
        onClick={() => setPersistedViewMode('card')}
        aria-pressed={viewMode === 'card'}
      >
        <RiNodeTree className="size-3.5" />
        Live
      </Button>
      <Button
        size="sm"
        variant={viewMode === 'table' ? 'secondary' : 'ghost'}
        className="h-6 gap-1 px-2 text-xs"
        onClick={() => setPersistedViewMode('table')}
        aria-pressed={viewMode === 'table'}
      >
        <RiListCheck className="size-3.5" />
        Table
      </Button>
    </div>
  )
  const columnsMenu =
    viewMode === 'table' ? (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            aria-label="Toggle columns"
          >
            <RiLayoutColumnLine className="size-3.5" />
            Columns
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuLabel>Toggle columns</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {TRUNK_TABLE_COLUMN_TOGGLES.map((column) => {
            const locked = 'locked' in column && column.locked
            return (
              <DropdownMenuCheckboxItem
                key={column.id}
                checked={isColumnVisible(columnVisibility, column.id)}
                disabled={locked}
                onCheckedChange={(checked) => {
                  if (locked) return
                  toggleColumnVisibility(column.id, checked === true)
                }}
                onSelect={(event) => event.preventDefault()}
              >
                {column.label}
              </DropdownMenuCheckboxItem>
            )
          })}
        </DropdownMenuContent>
      </DropdownMenu>
    ) : null

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header
        subBar={
          <div className="flex flex-wrap items-center gap-2 text-xs">
            {viewToggle}
            {renderSearch('h-8 min-w-0 flex-1 text-xs')}
            {sortSelect}
            {columnsMenu}
          </div>
        }
      >
        <div className="flex items-center gap-2 text-xs">
          {/* Search, sort, view and columns move to the mobile sub bar. */}
          <div className="hidden sm:contents">
            {renderSearch('h-8 w-48 text-xs')}
            {sortSelect}
          </div>
          {viewMode === 'table' ? (
            <PageActiveCallsChip
              totalCalls={pageActiveCalls}
              trunksOnCall={pageTrunksOnCall}
            />
          ) : null}
          <div className="hidden sm:contents">
            <Separator orientation="vertical" className="h-4" />
            {viewToggle}
            {columnsMenu}
          </div>
          <Separator orientation="vertical" className="hidden h-4 sm:block" />
          {/* Add Trunk */}
          <Button
            size="sm"
            variant="default"
            className="h-7 gap-1 bg-cyan-600 px-2 text-xs text-white hover:bg-cyan-700 dark:bg-cyan-500 dark:text-cyan-950 dark:hover:bg-cyan-400"
            onClick={() => {
              setCreateForm(newCreateForm())
              setCreateOpen(true)
            }}
            aria-label="Add trunk"
          >
            <RiAddLine className="size-3.5" />
            <span className="hidden sm:inline">Add Trunk</span>
          </Button>
          <Separator orientation="vertical" className="hidden h-4 sm:block" />
          {/* Refresh */}
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={handleRefresh}
            disabled={refreshing}
            aria-label="Refresh trunks"
          >
            <RiRefreshLine
              className={`size-3.5 ${refreshing ? 'animate-spin' : ''}`}
            />
            <span className="hidden sm:inline">Refresh</span>
          </Button>
          <Separator orientation="vertical" className="hidden h-4 sm:block" />
          {/* Theme toggle */}
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            onClick={toggleTheme}
            aria-label={
              theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'
            }
          >
            {theme === 'dark' ? (
              <RiSunLine className="size-3.5" />
            ) : (
              <RiMoonLine className="size-3.5" />
            )}
          </Button>
        </div>
      </Header>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4">
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : trunks.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiServerLine className="size-10 opacity-30" />
            <p className="text-sm">No trunks found</p>
          </div>
        ) : viewMode === 'card' ? (
          <LiveBoardErrorBoundary
            onSwitchToTable={() => setPersistedViewMode('table')}
          >
            <TrunkLiveBoard
              trunks={trunks}
              sessions={liveData.sessions}
              clients={liveData.clients}
              filter={liveFilter}
              onFilterChange={setLiveFilter}
              sessionsFetchedAt={liveData.sessionsFetchedAt}
              liveWarning={liveWarning}
              renderActions={(row) => (
                <TrunkActionsMenu
                  trunk={row.trunk}
                  onEdit={openEditModal}
                  onRegister={openRegisterModal}
                  onUnregister={openUnregisterModal}
                  onTestPush={openTestPushModal}
                  onSoftDelete={openSoftDeleteModal}
                  onRestore={openRestoreModal}
                />
              )}
            />
          </LiveBoardErrorBoundary>
        ) : (
          <TrunkTable
            trunks={trunks}
            columnVisibility={columnVisibility}
            onColumnVisibilityChange={handleColumnVisibilityChange}
            onEdit={openEditModal}
            onRegister={openRegisterModal}
            onUnregister={openUnregisterModal}
            onTestPush={openTestPushModal}
            onSoftDelete={openSoftDeleteModal}
            onRestore={openRestoreModal}
          />
        )}

        {viewMode === 'table' ? (
          <ServerPaginationControls
            page={page}
            totalPages={totalPages}
            total={total}
            pageSize={pageSize}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
            totalLabel="total"
          />
        ) : null}
      </div>
      {/* Register confirmation modal */}
      <Dialog
        open={!!registerTarget}
        onOpenChange={(open) => {
          if (!open) setRegisterTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Register Trunk</DialogTitle>
            <DialogDescription>
              Are you sure you want to register trunk{' '}
              <strong>{trunkIdentityText(registerTarget)}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRegisterTarget(null)}
              disabled={registering}
            >
              Cancel
            </Button>
            <Button onClick={confirmRegister} disabled={registering}>
              {registering ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Register
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit modal */}
      <Dialog
        open={!!editTarget}
        onOpenChange={(open) => {
          if (!open) closeEditModal()
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit Trunk</DialogTitle>
            <DialogDescription>
              Update trunk <strong>{trunkIdentityText(editTarget)}</strong>.
              Password is optional; leave empty to keep current password.
            </DialogDescription>
          </DialogHeader>
          {editForm ? (
            <div className="grid gap-2 py-1">
              <Input
                value={editForm.name}
                onChange={(e) =>
                  setEditForm((prev) =>
                    prev ? { ...prev, name: e.target.value } : prev,
                  )
                }
                placeholder="Name"
              />
              <Input
                value={editForm.domain}
                onChange={(e) =>
                  setEditForm((prev) =>
                    prev ? { ...prev, domain: e.target.value } : prev,
                  )
                }
                placeholder="Domain"
              />
              <Input
                value={editForm.port}
                onChange={(e) =>
                  setEditForm((prev) =>
                    prev ? { ...prev, port: e.target.value } : prev,
                  )
                }
                placeholder="Port"
                inputMode="numeric"
              />
              <Input
                value={editForm.username}
                onChange={(e) =>
                  setEditForm((prev) =>
                    prev ? { ...prev, username: e.target.value } : prev,
                  )
                }
                placeholder="Username"
              />
              <Input
                type="password"
                value={editForm.password}
                onChange={(e) =>
                  setEditForm((prev) =>
                    prev ? { ...prev, password: e.target.value } : prev,
                  )
                }
                placeholder="Password (optional replace)"
              />
              <div className="grid gap-1">
                <label className="text-xs text-muted-foreground">
                  Transport
                </label>
                <Select
                  value={editForm.transport}
                  onValueChange={(value) =>
                    setEditForm((prev) =>
                      prev
                        ? {
                            ...prev,
                            transport: value === 'udp' ? 'udp' : 'tcp',
                          }
                        : prev,
                    )
                  }
                >
                  <SelectTrigger
                    className="h-9 w-full text-sm"
                    aria-label="Transport"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="tcp">TCP</SelectItem>
                    <SelectItem value="udp">UDP</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={editForm.enabled}
                  onChange={(e) =>
                    setEditForm((prev) =>
                      prev ? { ...prev, enabled: e.target.checked } : prev,
                    )
                  }
                />
                Enabled
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={editForm.isDefault}
                  onChange={(e) =>
                    setEditForm((prev) =>
                      prev ? { ...prev, isDefault: e.target.checked } : prev,
                    )
                  }
                />
                Default trunk
              </label>
            </div>
          ) : null}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={closeEditModal}
              disabled={savingEdit}
            >
              Cancel
            </Button>
            <Button onClick={confirmEdit} disabled={savingEdit}>
              {savingEdit ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!testPushTarget}
        onOpenChange={(open) => {
          if (!open) setTestPushTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Test incoming push</DialogTitle>
            <DialogDescription>
              Send a fake incoming-call notification to{' '}
              <strong>{trunkIdentityText(testPushTarget)}</strong>? This is not
              a real SIP call, so answering will expire.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <p className="text-sm font-medium">Payload style</p>
            <div className="grid min-w-0 gap-2 sm:grid-cols-2">
              <Button
                type="button"
                variant={testPushStyle === 'data' ? 'default' : 'outline'}
                className="h-auto w-full min-w-0 shrink whitespace-normal flex-col items-start gap-1 px-3 py-2 text-left"
                onClick={() => setTestPushStyle('data')}
                disabled={testingPush}
              >
                <span className="text-sm font-medium">Data</span>
                <span className="block w-full text-[11px] font-normal leading-snug opacity-80">
                  Silent incoming_call payload. No tray text on Android.
                </span>
              </Button>
              <Button
                type="button"
                variant={testPushStyle === 'message' ? 'default' : 'outline'}
                className="h-auto w-full min-w-0 shrink whitespace-normal flex-col items-start gap-1 px-3 py-2 text-left"
                onClick={() => setTestPushStyle('message')}
                disabled={testingPush}
              >
                <span className="text-sm font-medium">Message</span>
                <span className="block w-full text-[11px] font-normal leading-snug opacity-80">
                  Visible notification title and body plus the same data.
                </span>
              </Button>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setTestPushTarget(null)}
              disabled={testingPush}
            >
              Cancel
            </Button>
            <Button onClick={confirmTestPush} disabled={testingPush}>
              {testingPush ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Send test push
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {/* Unregister confirmation modal */}
      <Dialog
        open={!!unregisterTarget}
        onOpenChange={(open) => {
          if (!open) setUnregisterTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Unregister Trunk</DialogTitle>
            <DialogDescription>
              Are you sure you want to unregister trunk{' '}
              <strong>{trunkIdentityText(unregisterTarget)}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setUnregisterTarget(null)}
              disabled={unregistering}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={confirmUnregister}
              disabled={unregistering}
            >
              {unregistering ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Unregister
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Soft delete confirmation modal */}
      <Dialog
        open={!!softDeleteTarget}
        onOpenChange={(open) => {
          if (!open) setSoftDeleteTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Disable Trunk</DialogTitle>
            <DialogDescription>
              Are you sure you want to disable trunk{' '}
              <strong>{trunkIdentityText(softDeleteTarget)}</strong>? You can
              restore it later.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setSoftDeleteTarget(null)}
              disabled={softDeleting}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={confirmSoftDelete}
              disabled={softDeleting}
            >
              {softDeleting ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Disable
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Restore confirmation modal */}
      <Dialog
        open={!!restoreTarget}
        onOpenChange={(open) => {
          if (!open) setRestoreTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Restore Trunk</DialogTitle>
            <DialogDescription>
              Are you sure you want to restore trunk{' '}
              <strong>{trunkIdentityText(restoreTarget)}</strong>?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRestoreTarget(null)}
              disabled={restoring}
            >
              Cancel
            </Button>
            <Button onClick={confirmRestore} disabled={restoring}>
              {restoring ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Restore
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create trunk modal */}
      <Dialog
        open={createOpen}
        onOpenChange={(open) => {
          if (!open) {
            setCreateOpen(false)
            setCreateForm(newCreateForm())
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Trunk</DialogTitle>
            <DialogDescription>
              Create a new SIP trunk. All fields are required.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2 py-1">
            <Input
              value={createForm.name}
              onChange={(e) =>
                setCreateForm((prev) => ({ ...prev, name: e.target.value }))
              }
              placeholder="Name"
            />
            <Input
              value={createForm.domain}
              onChange={(e) =>
                setCreateForm((prev) => ({ ...prev, domain: e.target.value }))
              }
              placeholder="Domain"
            />
            <Input
              value={createForm.port}
              onChange={(e) =>
                setCreateForm((prev) => ({ ...prev, port: e.target.value }))
              }
              placeholder="Port"
              inputMode="numeric"
            />
            <Input
              value={createForm.username}
              onChange={(e) =>
                setCreateForm((prev) => ({
                  ...prev,
                  username: e.target.value,
                }))
              }
              placeholder="Username"
            />
            <Input
              type="password"
              value={createForm.password}
              onChange={(e) =>
                setCreateForm((prev) => ({
                  ...prev,
                  password: e.target.value,
                }))
              }
              placeholder="Password"
            />
            <div className="grid gap-1">
              <label className="text-xs text-muted-foreground">Transport</label>
              <Select
                value={createForm.transport}
                onValueChange={(value) =>
                  setCreateForm((prev) => ({
                    ...prev,
                    transport: value === 'udp' ? 'udp' : 'tcp',
                  }))
                }
              >
                <SelectTrigger
                  className="h-9 w-full text-sm"
                  aria-label="Transport"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="tcp">TCP</SelectItem>
                  <SelectItem value="udp">UDP</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={createForm.enabled}
                onChange={(e) =>
                  setCreateForm((prev) => ({
                    ...prev,
                    enabled: e.target.checked,
                  }))
                }
              />
              Enabled
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={createForm.isDefault}
                onChange={(e) =>
                  setCreateForm((prev) => ({
                    ...prev,
                    isDefault: e.target.checked,
                  }))
                }
              />
              Default trunk
            </label>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setCreateOpen(false)
                setCreateForm(newCreateForm())
              }}
              disabled={savingCreate}
            >
              Cancel
            </Button>
            <Button onClick={confirmCreate} disabled={savingCreate}>
              {savingCreate ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function TrunkTable({
  trunks,
  columnVisibility,
  onColumnVisibilityChange,
  onEdit,
  onRegister,
  onUnregister,
  onTestPush,
  onSoftDelete,
  onRestore,
}: {
  trunks: Array<Trunk>
  columnVisibility: VisibilityState
  onColumnVisibilityChange: OnChangeFn<VisibilityState>
  onEdit: (trunk: Trunk) => void
  onRegister: (trunk: Trunk) => void
  onUnregister: (trunk: Trunk) => void
  onTestPush: (trunk: Trunk) => void
  onSoftDelete: (trunk: Trunk) => void
  onRestore: (trunk: Trunk) => void
}) {
  const columns = useMemo<Array<ColumnDef<Trunk>>>(
    () => [
      {
        accessorKey: 'id',
        header: 'ID',
        cell: ({ row }) => (
          <span className="text-muted-foreground">#{row.original.id}</span>
        ),
      },
      {
        accessorKey: 'name',
        header: 'Name',
        enableHiding: false,
        cell: ({ row }) => (
          <span className="font-medium">{row.original.name}</span>
        ),
      },
      {
        id: 'inUseBy',
        header: 'In Use By',
        cell: ({ row }) => (
          <span className="text-muted-foreground">
            {formatInUseBy(row.original)}
          </span>
        ),
      },
      {
        id: 'register',
        header: 'Register',
        cell: ({ row }) => (
          <div className="space-y-0.5">
            <Badge
              variant={row.original.isRegistered ? 'success' : 'secondary'}
              className="text-[10px]"
            >
              {row.original.isRegistered ? 'Registered' : 'Unregistered'}
            </Badge>
          </div>
        ),
      },
      {
        id: 'pushContact',
        header: 'Push Contact',
        cell: ({ row }) => (
          <div className="space-y-0.5">
            <PushContactBadge trunk={row.original} />
            <div title={row.original.pnAppId || undefined}>
              <PushContactDetails trunk={row.original} />
            </div>
          </div>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        cell: ({ row }) => (
          <div className="flex items-center gap-1.5">
            {row.original.isDefault ? (
              <Badge variant="default" className="text-[10px]">
                Default
              </Badge>
            ) : null}
            <Badge
              variant={row.original.enabled ? 'success' : 'destructive'}
              className="text-[10px]"
            >
              {getTrunkStatusLabel(row.original.enabled)}
            </Badge>
          </div>
        ),
      },
      {
        accessorKey: 'domain',
        header: 'Domain',
      },
      {
        accessorKey: 'port',
        header: 'Port',
      },
      {
        accessorKey: 'username',
        header: 'Username',
      },
      {
        accessorKey: 'transport',
        header: 'Transport',
        cell: ({ row }) => row.original.transport.toUpperCase(),
      },
      {
        id: 'destination',
        header: 'Destination',
        cell: ({ row }) => formatDestinations(row.original.activeDestinations),
      },
      {
        id: 'calls',
        header: 'Calls',
        cell: ({ row }) => (
          <ActiveCallsDisplay count={row.original.activeCallCount} />
        ),
      },
      {
        accessorKey: 'lastRegisteredAt',
        header: 'Last Registered',
        cell: ({ row }) => (
          <span className="text-muted-foreground">
            {formatThaiDateTime(row.original.lastRegisteredAt)}
          </span>
        ),
      },
      {
        accessorKey: 'lastUnregisteredAt',
        header: 'Last Unregistered',
        cell: ({ row }) => (
          <span className="text-muted-foreground">
            {formatThaiDateTime(row.original.lastUnregisteredAt || '')}
          </span>
        ),
      },
      {
        id: 'lastOnline',
        header: 'Last Online',
        cell: ({ row }) => (
          <span className="text-muted-foreground">
            {row.original.lastOnlinePlatform
              ? `${row.original.lastOnlinePlatform} · ${formatThaiDateTime(row.original.lastOnlineAt || '')}`
              : '-'}
          </span>
        ),
      },
      {
        id: 'uid',
        header: 'UID',
        cell: ({ row }) => (
          <span className="font-mono text-[11px]">
            {formatUid(normalizeTrunkUid(row.original))}
          </span>
        ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        enableHiding: false,
        cell: ({ row }) => (
          <div className="flex justify-end gap-1">
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => onEdit(row.original)}
            >
              Edit
            </Button>
            <Button
              size="sm"
              variant="secondary"
              className="h-6 px-2 text-[10px]"
              onClick={() => onRegister(row.original)}
              disabled={isRegisterActionDisabled(row.original)}
            >
              Register
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => onUnregister(row.original)}
              disabled={!row.original.isRegistered}
            >
              Unregister
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => onTestPush(row.original)}
              disabled={!canTestIncomingPush(row.original)}
            >
              Test push
            </Button>
            {row.original.enabled ? (
              <Button
                size="sm"
                variant="destructive"
                className="h-6 px-2 text-[10px]"
                onClick={() => onSoftDelete(row.original)}
              >
                {getTrunkLifecycleActionLabel(row.original.enabled)}
              </Button>
            ) : (
              <Button
                size="sm"
                variant="default"
                className="h-6 px-2 text-[10px]"
                onClick={() => onRestore(row.original)}
              >
                {getTrunkLifecycleActionLabel(row.original.enabled)}
              </Button>
            )}
          </div>
        ),
      },
    ],
    [onEdit, onRegister, onRestore, onSoftDelete, onTestPush, onUnregister],
  )

  return (
    <DataTable
      columns={columns}
      data={trunks}
      columnVisibility={columnVisibility}
      onColumnVisibilityChange={onColumnVisibilityChange}
      getRowClassName={trunkTableRowClassName}
    />
  )
}

type TrunkActionHandlers = {
  onEdit: (trunk: Trunk) => void
  onRegister: (trunk: Trunk) => void
  onUnregister: (trunk: Trunk) => void
  onTestPush: (trunk: Trunk) => void
  onSoftDelete: (trunk: Trunk) => void
  onRestore: (trunk: Trunk) => void
}

function TrunkActionsMenu({
  trunk,
  onEdit,
  onRegister,
  onUnregister,
  onTestPush,
  onSoftDelete,
  onRestore,
}: TrunkActionHandlers & { trunk: Trunk }) {
  const showRegister = !isRegisterActionDisabled(trunk)
  return (
    <div className="flex shrink-0 items-center gap-1">
      {showRegister ? (
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-2 text-xs"
          onClick={() => onRegister(trunk)}
        >
          Register
        </Button>
      ) : null}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            aria-label={`Actions for ${trunk.name}`}
          >
            <RiMore2Fill className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuLabel className="truncate">
            {trunk.name}
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            disabled={isRegisterActionDisabled(trunk)}
            onSelect={() => onRegister(trunk)}
          >
            Register
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!trunk.isRegistered}
            onSelect={() => onUnregister(trunk)}
          >
            Unregister
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onEdit(trunk)}>
            Edit
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={!canTestIncomingPush(trunk)}
            onSelect={() => onTestPush(trunk)}
          >
            Test push
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {trunk.enabled ? (
            <DropdownMenuItem
              variant="destructive"
              onSelect={() => onSoftDelete(trunk)}
            >
              {getTrunkLifecycleActionLabel(trunk.enabled)}
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem onSelect={() => onRestore(trunk)}>
              {getTrunkLifecycleActionLabel(trunk.enabled)}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
