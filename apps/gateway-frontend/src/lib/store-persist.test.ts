// @vitest-environment jsdom
import { Store } from '@tanstack/store'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { attachPersist, clearPersisted, loadPersisted } from './store-persist'

// ---------- helpers ----------
beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ---------- loadPersisted ----------
describe('loadPersisted', () => {
  it('returns null when key is missing', () => {
    expect(loadPersisted('nope', 1)).toBeNull()
  })

  it('returns data when version matches', () => {
    const data = { foo: 'bar' }
    localStorage.setItem('k', JSON.stringify({ version: 1, data }))
    expect(loadPersisted('k', 1)).toEqual(data)
  })

  it('returns null when version mismatches', () => {
    localStorage.setItem('k', JSON.stringify({ version: 1, data: {} }))
    expect(loadPersisted('k', 2)).toBeNull()
  })

  it('returns null on corrupt JSON', () => {
    localStorage.setItem('k', '{bad json')
    expect(loadPersisted('k', 1)).toBeNull()
  })

  it('returns null when envelope is not an object', () => {
    localStorage.setItem('k', '"just a string"')
    expect(loadPersisted('k', 1)).toBeNull()
  })
})

// ---------- clearPersisted ----------
describe('clearPersisted', () => {
  it('removes the key from localStorage', () => {
    localStorage.setItem('k', 'value')
    expect(localStorage.getItem('k')).toBe('value')
    clearPersisted('k')
    expect(localStorage.getItem('k')).toBeNull()
  })
})

// ---------- attachPersist ----------
interface TestState {
  name: string
  age: number
  secret: string
  transient: number
}

interface TestPersisted {
  name: string
  age: number
  secret: string
}

function createTestStore(initial?: Partial<TestState>) {
  return new Store<TestState>({
    name: '',
    age: 0,
    secret: '',
    transient: 0,
    ...initial,
  })
}

const defaultOptions = {
  key: 'test-store',
  version: 1,
  debounceMs: 0, // instant for testing
  select: (s: TestState): TestPersisted => ({
    name: s.name,
    age: s.age,
    secret: s.secret,
  }),
  merge: (p: TestPersisted, c: TestState): TestState => ({
    ...c,
    name: p.name,
    age: p.age,
    secret: p.secret,
  }),
}

describe('attachPersist', () => {
  it('hydrates state from localStorage on attach', () => {
    const saved: TestPersisted = { name: 'Alice', age: 30, secret: 'pw' }
    localStorage.setItem(
      'test-store',
      JSON.stringify({ version: 1, data: saved }),
    )

    const store = createTestStore()
    const unsub = attachPersist(store, defaultOptions)

    expect(store.state.name).toBe('Alice')
    expect(store.state.age).toBe(30)
    expect(store.state.secret).toBe('pw')
    // transient should remain default
    expect(store.state.transient).toBe(0)

    unsub()
  })

  it('does not overwrite state when no saved data exists', () => {
    const store = createTestStore({ name: 'Bob', age: 25 })
    const unsub = attachPersist(store, defaultOptions)

    expect(store.state.name).toBe('Bob')
    expect(store.state.age).toBe(25)

    unsub()
  })

  it('persists selected fields to localStorage after state change', async () => {
    const store = createTestStore()
    const unsub = attachPersist(store, defaultOptions)

    store.setState((s) => ({ ...s, name: 'Charlie', transient: 999 }))

    // Wait for debounce (0ms, but still async setTimeout)
    await new Promise((r) => setTimeout(r, 10))

    const raw = localStorage.getItem('test-store')
    expect(raw).not.toBeNull()

    const envelope = JSON.parse(raw!)
    expect(envelope.version).toBe(1)
    expect(envelope.data.name).toBe('Charlie')
    // transient should NOT be in persisted data
    expect(envelope.data.transient).toBeUndefined()

    unsub()
  })

  it('persists password/secret fields when included in select', async () => {
    const store = createTestStore()
    const unsub = attachPersist(store, defaultOptions)

    store.setState((s) => ({ ...s, secret: 'super-secret-123' }))

    await new Promise((r) => setTimeout(r, 10))

    const raw = localStorage.getItem('test-store')
    const envelope = JSON.parse(raw!)
    expect(envelope.data.secret).toBe('super-secret-123')

    unsub()
  })

  it('skips write when serialized data has not changed', async () => {
    const store = createTestStore({ name: 'Same' })
    const spy = vi.spyOn(Storage.prototype, 'setItem')
    const unsub = attachPersist(store, defaultOptions)

    // Trigger a state change that doesn't affect persisted fields
    store.setState((s) => ({ ...s, transient: 42 }))
    await new Promise((r) => setTimeout(r, 10))

    store.setState((s) => ({ ...s, transient: 43 }))
    await new Promise((r) => setTimeout(r, 10))

    // Both transient changes produce the same persisted JSON, so at most 1 write.
    const writes = spy.mock.calls.filter(
      (args: Array<unknown>) => args[0] === 'test-store',
    )
    expect(writes.length).toBeLessThanOrEqual(1)

    spy.mockRestore()
    unsub()
  })

  it('unsubscribe stops further writes', async () => {
    const store = createTestStore()
    const unsub = attachPersist(store, defaultOptions)
    unsub()

    store.setState((s) => ({ ...s, name: 'After-unsub' }))
    await new Promise((r) => setTimeout(r, 10))

    const raw = localStorage.getItem('test-store')
    // Should be null or not contain 'After-unsub'
    if (raw) {
      const envelope = JSON.parse(raw)
      expect(envelope.data.name).not.toBe('After-unsub')
    }
  })

  it('ignores corrupt saved data gracefully', () => {
    localStorage.setItem('test-store', '{broken')
    const store = createTestStore({ name: 'Default' })
    const unsub = attachPersist(store, defaultOptions)

    // Should keep defaults, no throw
    expect(store.state.name).toBe('Default')

    unsub()
  })

  it('ignores saved data with wrong version', () => {
    localStorage.setItem(
      'test-store',
      JSON.stringify({ version: 99, data: { name: 'Old' } }),
    )
    const store = createTestStore({ name: 'Current' })
    const unsub = attachPersist(store, defaultOptions)

    expect(store.state.name).toBe('Current')

    unsub()
  })
})
