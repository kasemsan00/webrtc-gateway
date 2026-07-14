/**
 * Normalize a public base path for Vite and the production server.
 * Returns "/" for empty/root, otherwise a path with a leading slash and no trailing slash
 * except when formatting for Vite (see viteBaseFromEnv).
 */
export function normalizeBasePath(raw?: string | null): string {
  const trimmed = (raw ?? '').trim()
  if (!trimmed || trimmed === '/') return '/'

  const withLeadingSlash = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  const withoutTrailing = withLeadingSlash.replace(/\/+$/, '')
  return withoutTrailing || '/'
}

/** Vite `base` requires a trailing slash for non-root paths. */
export function viteBaseFromEnv(raw?: string | null): string {
  const base = normalizeBasePath(raw)
  return base === '/' ? '/' : `${base}/`
}

/** TanStack Router `basepath` has no trailing slash (omit when root). */
export function routerBasePathFromEnv(raw?: string | null): string | undefined {
  const base = normalizeBasePath(raw)
  return base === '/' ? undefined : base
}
