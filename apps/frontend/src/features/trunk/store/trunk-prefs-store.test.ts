// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_COLUMN_VISIBILITY,
  initializeTrunkPrefsStore,
  mergeTrunkPrefs,
  resetTrunkPrefsStoreForTests,
  setColumnVisibility,
  setViewMode,
  toggleColumnVisibility,
  trunkPrefsStore,
} from './trunk-prefs-store'

type StorageMock = {
  clear: () => void
  getItem: (key: string) => string | null
  key: (index: number) => string | null
  removeItem: (key: string) => void
  setItem: (key: string, value: string) => void
  readonly length: number
}

function createStorageMock(): StorageMock {
  const values = new Map<string, string>()
  return {
    clear: () => values.clear(),
    getItem: (key) => values.get(String(key)) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => {
      values.delete(String(key))
    },
    setItem: (key, value) => {
      values.set(String(key), String(value))
    },
    get length() {
      return values.size
    },
  }
}

beforeEach(() => {
  const storage = createStorageMock()
  vi.stubGlobal('localStorage', storage)
  Object.defineProperty(window, 'localStorage', {
    value: storage,
    configurable: true,
  })
  resetTrunkPrefsStoreForTests()
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  resetTrunkPrefsStoreForTests()
})

describe('mergeTrunkPrefs', () => {
  const current = {
    viewMode: 'card' as const,
    columnVisibility: { ...DEFAULT_COLUMN_VISIBILITY },
  }

  it('keeps current state for invalid payloads', () => {
    expect(mergeTrunkPrefs(null, current)).toEqual(current)
    expect(mergeTrunkPrefs('card', current)).toEqual(current)
    expect(mergeTrunkPrefs([], current)).toEqual(current)
  })

  it('merges viewMode and columnVisibility', () => {
    expect(
      mergeTrunkPrefs(
        {
          viewMode: 'table',
          columnVisibility: { uid: true, domain: false },
        },
        current,
      ),
    ).toEqual({
      viewMode: 'table',
      columnVisibility: {
        uid: true,
        domain: false,
        name: true,
        actions: true,
      },
    })
  })

  it('forces name and actions visible when merging', () => {
    expect(
      mergeTrunkPrefs(
        {
          viewMode: 'card',
          columnVisibility: { name: false, actions: false, uid: false },
        },
        current,
      ).columnVisibility,
    ).toEqual({
      uid: false,
      name: true,
      actions: true,
    })
  })
})

describe('trunkPrefsStore column visibility', () => {
  it('defaults uid to hidden', () => {
    expect(trunkPrefsStore.state.columnVisibility).toEqual({
      uid: false,
    })
  })

  it('toggles column visibility and ignores locked columns', () => {
    toggleColumnVisibility('domain', false)
    expect(trunkPrefsStore.state.columnVisibility.domain).toBe(false)

    toggleColumnVisibility('name', false)
    toggleColumnVisibility('actions', false)
    expect(trunkPrefsStore.state.columnVisibility.name).toBeUndefined()
    expect(trunkPrefsStore.state.columnVisibility.actions).toBeUndefined()
  })

  it('setColumnVisibility always keeps name and actions visible', () => {
    setColumnVisibility({
      uid: true,
      name: false,
      actions: false,
      port: false,
    })
    expect(trunkPrefsStore.state.columnVisibility).toEqual({
      uid: true,
      name: true,
      actions: true,
      port: false,
    })
  })

  it('hydrates persisted prefs on initialize', async () => {
    localStorage.setItem(
      'k2_trunk_prefs',
      JSON.stringify({
        version: 2,
        data: {
          viewMode: 'table',
          columnVisibility: { uid: true, calls: false },
        },
      }),
    )

    initializeTrunkPrefsStore()

    expect(trunkPrefsStore.state.viewMode).toBe('table')
    expect(trunkPrefsStore.state.columnVisibility).toEqual({
      uid: true,
      calls: false,
      name: true,
      actions: true,
    })
  })

  it('persists column visibility after toggle', async () => {
    initializeTrunkPrefsStore()
    setViewMode('table')
    toggleColumnVisibility('username', false)

    await vi.waitFor(() => {
      const raw = localStorage.getItem('k2_trunk_prefs')
      expect(raw).toBeTruthy()
      const envelope = JSON.parse(raw!) as {
        version: number
        data: {
          viewMode: string
          columnVisibility: Record<string, boolean>
        }
      }
      expect(envelope.version).toBe(2)
      expect(envelope.data.viewMode).toBe('table')
      expect(envelope.data.columnVisibility.username).toBe(false)
      expect(envelope.data.columnVisibility.uid).toBe(false)
    })
  })
})
