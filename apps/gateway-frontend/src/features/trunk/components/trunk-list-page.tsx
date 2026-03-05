import {
  RiAddLine,
  RiGridLine,
  RiListCheck,
  RiLoader4Line,
  RiMoonLine,
  RiPhoneLine,
  RiRefreshLine,
  RiServerLine,
  RiSunLine,
} from '@remixicon/react'
import { debounce } from '@tanstack/pacer'
import { useStore } from '@tanstack/react-store'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import type { ColumnDef } from '@tanstack/react-table'

import type {
  CreateTrunkPayload,
  Trunk,
  TrunkListParams,
  UpdateTrunkPayload,
} from '@/features/trunk/types'
import { normalizeTrunkUid } from '@/features/trunk/types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { DataTable } from '@/components/ui/data-table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import { useTheme } from '@/lib/theme'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import {
  createTrunk,
  deleteTrunk,
  fetchTrunks,
  refreshTrunks,
  registerTrunk,
  subscribeTrunkEvents,
  unregisterTrunk,
  updateTrunk,
} from '@/features/trunk/services/trunk-api'
import {
  initializeTrunkPrefsStore,
  setViewMode as setPersistedViewMode,
  trunkPrefsStore,
} from '@/features/trunk/store/trunk-prefs-store'

const DEFAULT_PAGE_SIZE = 20

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

function trunkIdentityText(trunk?: Trunk | null) {
  if (!trunk) return '-'
  return `${trunk.name} (#${trunk.id}, uid: ${formatUid(normalizeTrunkUid(trunk))})`
}

export function isRegisterActionDisabled(trunk: Trunk) {
  return trunk.isRegistered || !trunk.enabled
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
    transport: 'tcp',
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
  const [trunks, setTrunks] = useState<Array<Trunk>>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [sortMode, setSortMode] = useState<TrunkSortMode>('activeCallsDesc')
  const { viewMode } = useStore(trunkPrefsStore, (state) => state)

  useEffect(() => {
    initializeTrunkPrefsStore()
  }, [])
  const [registerTarget, setRegisterTarget] = useState<Trunk | null>(null)
  const [registering, setRegistering] = useState(false)
  const [unregisterTarget, setUnregisterTarget] = useState<Trunk | null>(null)
  const [unregistering, setUnregistering] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Trunk | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [editTarget, setEditTarget] = useState<Trunk | null>(null)
  const [editForm, setEditForm] = useState<TrunkEditForm | null>(null)
  const [savingEdit, setSavingEdit] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<TrunkCreateForm>(newCreateForm)
  const [savingCreate, setSavingCreate] = useState(false)

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

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
        const res = await fetchTrunks({
          page: params?.page ?? page,
          pageSize: params?.pageSize ?? pageSize,
          search: (params?.search ?? searchRef.current) || undefined,
          sortBy: sortParams.sortBy,
          sortDir: sortParams.sortDir,
        })
        setTrunks(res.items)
        setTotal(res.total)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch trunks')
      } finally {
        if (!options?.silent) {
          setLoading(false)
        }
      }
    },
    [page, pageSize, getSortParams],
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
        description: 'Trunk list has been updated',
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
        description:
          err instanceof Error ? err.message : 'Failed to register trunk',
      })
    } finally {
      setRegistering(false)
    }
  }, [registerTarget, load])

  const openUnregisterModal = useCallback((trunk: Trunk) => {
    setUnregisterTarget(trunk)
  }, [])

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

  const openDeleteModal = useCallback((trunk: Trunk) => {
    setDeleteTarget(trunk)
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

  const confirmDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteTrunk(deleteTarget.id)
      setDeleteTarget(null)
      await load()
      toast.success('Delete success', {
        description: trunkIdentityText(deleteTarget),
      })
    } catch (err) {
      toast.error('Delete failed', {
        description:
          err instanceof Error ? err.message : 'Failed to delete trunk',
      })
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, load])

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

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          {/* Search */}
          <Input
            className="h-8 w-48 text-xs"
            placeholder="Search username or name..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              debouncedSearch(e.target.value)
            }}
          />
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
          <Separator orientation="vertical" className="h-4" />
          {/* View mode toggle */}
          <div className="flex items-center gap-0.5">
            <Button
              size="icon"
              variant={viewMode === 'card' ? 'secondary' : 'ghost'}
              className="size-7"
              onClick={() => setPersistedViewMode('card')}
              aria-label="Card view"
            >
              <RiGridLine className="size-3.5" />
            </Button>
            <Button
              size="icon"
              variant={viewMode === 'table' ? 'secondary' : 'ghost'}
              className="size-7"
              onClick={() => setPersistedViewMode('table')}
              aria-label="Table view"
            >
              <RiListCheck className="size-3.5" />
            </Button>
          </div>
          <Separator orientation="vertical" className="h-4" />
          {/* Add Trunk */}
          <Button
            size="sm"
            variant="default"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => {
              setCreateForm(newCreateForm())
              setCreateOpen(true)
            }}
          >
            <RiAddLine className="size-3.5" />
            Add Trunk
          </Button>
          <Separator orientation="vertical" className="h-4" />
          {/* Refresh */}
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={handleRefresh}
            disabled={refreshing}
          >
            <RiRefreshLine
              className={`size-3.5 ${refreshing ? 'animate-spin' : ''}`}
            />
            Refresh
          </Button>
          <Separator orientation="vertical" className="h-4" />
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
          <div className="grid gap-3 sm:grid-cols-1 md:grid-cols-2 xl:grid-cols-3">
            {trunks.map((trunk) => (
              <TrunkCard
                key={trunk.id}
                trunk={trunk}
                onEdit={openEditModal}
                onRegister={openRegisterModal}
                onUnregister={openUnregisterModal}
                onDelete={openDeleteModal}
              />
            ))}
          </div>
        ) : (
          <TrunkTable
            trunks={trunks}
            onEdit={openEditModal}
            onRegister={openRegisterModal}
            onUnregister={openUnregisterModal}
            onDelete={openDeleteModal}
          />
        )}

        <ServerPaginationControls
          page={page}
          totalPages={totalPages}
          total={total}
          pageSize={pageSize}
          onPageChange={handlePageChange}
          onPageSizeChange={handlePageSizeChange}
          totalLabel="total"
        />
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

      {/* Delete confirmation modal */}
      <Dialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Trunk</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete trunk{' '}
              <strong>{trunkIdentityText(deleteTarget)}</strong>? This cannot be
              undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeleteTarget(null)}
              disabled={deleting}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={confirmDelete}
              disabled={deleting}
            >
              {deleting ? (
                <RiLoader4Line className="mr-1 size-3.5 animate-spin" />
              ) : null}
              Delete
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
  onEdit,
  onRegister,
  onUnregister,
  onDelete,
}: {
  trunks: Array<Trunk>
  onEdit: (trunk: Trunk) => void
  onRegister: (trunk: Trunk) => void
  onUnregister: (trunk: Trunk) => void
  onDelete: (trunk: Trunk) => void
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
        id: 'uid',
        header: 'UID',
        cell: ({ row }) => (
          <span className="font-mono text-[11px]">
            {formatUid(normalizeTrunkUid(row.original))}
          </span>
        ),
      },
      {
        accessorKey: 'name',
        header: 'Name',
        cell: ({ row }) => (
          <span className="font-medium">{row.original.name}</span>
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
          <span className="flex items-center gap-1">
            <RiPhoneLine className="size-3 text-cyan-400" />
            {row.original.activeCallCount}
          </span>
        ),
      },
      {
        id: 'register',
        header: 'Register',
        cell: ({ row }) => (
          <Badge
            variant={row.original.isRegistered ? 'success' : 'secondary'}
            className="text-[10px]"
          >
            {row.original.isRegistered ? 'Registered' : 'Unregistered'}
          </Badge>
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
              {row.original.enabled ? 'Enabled' : 'Disabled'}
            </Badge>
          </div>
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
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
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
              variant="destructive"
              className="h-6 px-2 text-[10px]"
              onClick={() => onDelete(row.original)}
            >
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [onDelete, onEdit, onRegister, onUnregister],
  )

  return <DataTable columns={columns} data={trunks} />
}

function TrunkCard({
  trunk,
  onEdit,
  onRegister,
  onUnregister,
  onDelete,
}: {
  trunk: Trunk
  onEdit: (trunk: Trunk) => void
  onRegister: (trunk: Trunk) => void
  onUnregister: (trunk: Trunk) => void
  onDelete: (trunk: Trunk) => void
}) {
  return (
    <Card className="border-border/60">
      <CardContent className="space-y-2 p-3">
        {/* Header row */}
        <div className="flex items-center justify-between">
          <div className="min-w-0">
            <span className="text-sm font-semibold">{trunk.name}</span>
            <p className="truncate font-mono text-[11px] text-muted-foreground">
              #{trunk.id} · uid: {formatUid(normalizeTrunkUid(trunk))}
            </p>
          </div>
          <div className="flex items-center gap-1.5">
            {trunk.isDefault ? (
              <Badge variant="default" className="text-[10px]">
                Default
              </Badge>
            ) : null}
            <Badge
              variant={trunk.enabled ? 'success' : 'destructive'}
              className="text-[10px]"
            >
              {trunk.enabled ? 'Enabled' : 'Disabled'}
            </Badge>
          </div>
        </div>

        <Separator />

        {/* Details */}
        <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          <Detail label="Domain" value={trunk.domain} />
          <Detail label="Port" value={String(trunk.port)} />
          <Detail label="Username" value={trunk.username} />
          <Detail label="Transport" value={trunk.transport.toUpperCase()} />
          <Detail
            label="Destination"
            value={formatDestinations(trunk.activeDestinations)}
          />
          <Detail
            label="Active Calls"
            value={
              <span className="flex items-center gap-1">
                <RiPhoneLine className="size-3 text-cyan-400" />
                {trunk.activeCallCount}
              </span>
            }
          />
          <Detail label="Lease Owner" value={trunk.leaseOwner || '-'} />
          <Detail
            label="Register"
            value={
              <Badge
                variant={trunk.isRegistered ? 'success' : 'secondary'}
                className="text-[10px]"
              >
                {trunk.isRegistered ? 'Registered' : 'Unregistered'}
              </Badge>
            }
          />
          <Detail
            label="Last Registered"
            value={formatThaiDateTime(trunk.lastRegisteredAt)}
          />
          <Detail
            label="Lease Until"
            value={formatThaiDateTime(trunk.leaseUntil)}
          />
        </div>

        {trunk.lastError ? (
          <div className="rounded bg-red-500/10 px-2 py-1 text-[11px] text-red-400">
            {trunk.lastError}
          </div>
        ) : null}

        <Separator />

        {/* Footer */}
        <div className="flex items-center justify-between">
          <span className="text-[10px] text-muted-foreground">
            Created {formatThaiDateTime(trunk.createdAt)}
          </span>
          <div className="flex gap-1">
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => onEdit(trunk)}
            >
              Edit
            </Button>
            <Button
              size="sm"
              variant="secondary"
              className="h-6 px-2 text-[10px]"
              onClick={() => onRegister(trunk)}
              disabled={isRegisterActionDisabled(trunk)}
            >
              Register
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-6 px-2 text-[10px]"
              onClick={() => onUnregister(trunk)}
              disabled={!trunk.isRegistered}
            >
              Unregister
            </Button>
            <Button
              size="sm"
              variant="destructive"
              className="h-6 px-2 text-[10px]"
              onClick={() => onDelete(trunk)}
            >
              Delete
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function Detail({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-1">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium">{value}</span>
    </div>
  )
}
