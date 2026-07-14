import { describe, expect, it } from 'vitest'

import {
  normalizeBasePath,
  routerBasePathFromEnv,
  viteBaseFromEnv,
} from './base-path'

describe('normalizeBasePath', () => {
  it('defaults empty values to root', () => {
    expect(normalizeBasePath(undefined)).toBe('/')
    expect(normalizeBasePath('')).toBe('/')
    expect(normalizeBasePath(' / ')).toBe('/')
    expect(normalizeBasePath('/')).toBe('/')
  })

  it('normalizes admin paths', () => {
    expect(normalizeBasePath('/admin')).toBe('/admin')
    expect(normalizeBasePath('/admin/')).toBe('/admin')
    expect(normalizeBasePath('admin')).toBe('/admin')
    expect(normalizeBasePath('admin/')).toBe('/admin')
  })
})

describe('viteBaseFromEnv', () => {
  it('returns root or trailing-slash base', () => {
    expect(viteBaseFromEnv(undefined)).toBe('/')
    expect(viteBaseFromEnv('/admin')).toBe('/admin/')
    expect(viteBaseFromEnv('/admin/')).toBe('/admin/')
  })
})

describe('routerBasePathFromEnv', () => {
  it('omits root and returns path without trailing slash', () => {
    expect(routerBasePathFromEnv(undefined)).toBeUndefined()
    expect(routerBasePathFromEnv('/')).toBeUndefined()
    expect(routerBasePathFromEnv('/admin/')).toBe('/admin')
  })
})
