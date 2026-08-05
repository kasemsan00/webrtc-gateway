import { Store } from '@tanstack/store'
import type { VisibilityState } from '@tanstack/react-table'

import { attachPersist } from '@/lib/store-persist'

type ViewMode = 'card' | 'table'

export interface TrunkPrefsState {
  viewMode: ViewMode
  columnVisibility: VisibilityState
}

export interface TrunkPrefsPersisted {
  viewMode: ViewMode
  columnVisibility: VisibilityState
}

const PERSIST_KEY = 'k2_trunk_prefs'
const PERSIST_VERSION = 2

export const DEFAULT_COLUMN_VISIBILITY: VisibilityState = {
  uid: false,
}

export const trunkPrefsStore = new Store<TrunkPrefsState>({
  viewMode: 'card',
  columnVisibility: { ...DEFAULT_COLUMN_VISIBILITY },
})

let initialized = false

function isViewMode(value: unknown): value is ViewMode {
  return value === 'card' || value === 'table'
}

function isVisibilityState(value: unknown): value is VisibilityState {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return false
  }
  return Object.values(value).every((entry) => typeof entry === 'boolean')
}

export function mergeTrunkPrefs(
  persisted: unknown,
  current: TrunkPrefsState,
): TrunkPrefsState {
  if (!persisted || typeof persisted !== 'object' || Array.isArray(persisted)) {
    return current
  }

  const raw = persisted as Partial<TrunkPrefsPersisted>
  const next: TrunkPrefsState = { ...current }

  if (isViewMode(raw.viewMode)) {
    next.viewMode = raw.viewMode
  }

  if (isVisibilityState(raw.columnVisibility)) {
    next.columnVisibility = {
      ...DEFAULT_COLUMN_VISIBILITY,
      ...raw.columnVisibility,
      // Name and Actions are always visible.
      name: true,
      actions: true,
    }
  }

  return next
}

export function initializeTrunkPrefsStore() {
  if (typeof window === 'undefined' || initialized) return
  initialized = true

  attachPersist<TrunkPrefsState, TrunkPrefsPersisted>(trunkPrefsStore, {
    key: PERSIST_KEY,
    version: PERSIST_VERSION,
    debounceMs: 200,
    select: (state) => ({
      viewMode: state.viewMode,
      columnVisibility: state.columnVisibility,
    }),
    merge: mergeTrunkPrefs,
  })
}

export function setViewMode(mode: ViewMode) {
  trunkPrefsStore.setState((state) => ({
    ...state,
    viewMode: mode,
  }))
}

export function setColumnVisibility(visibility: VisibilityState) {
  trunkPrefsStore.setState((state) => ({
    ...state,
    columnVisibility: {
      ...visibility,
      name: true,
      actions: true,
    },
  }))
}

export function toggleColumnVisibility(columnId: string, visible: boolean) {
  if (columnId === 'name' || columnId === 'actions') return

  trunkPrefsStore.setState((state) => ({
    ...state,
    columnVisibility: {
      ...state.columnVisibility,
      [columnId]: visible,
    },
  }))
}

/** Test-only: reset module init flag and store state. */
export function resetTrunkPrefsStoreForTests() {
  initialized = false
  trunkPrefsStore.setState(() => ({
    viewMode: 'card',
    columnVisibility: { ...DEFAULT_COLUMN_VISIBILITY },
  }))
}
