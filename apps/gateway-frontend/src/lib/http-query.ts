type QueryParamValue = string | number | boolean | null | undefined

type QueryParams = Record<string, QueryParamValue>

/**
 * Serializes a params object into a URLSearchParams, skipping any key whose
 * value is null, undefined, or an empty string.
 */
export function buildQuery(params: QueryParams): URLSearchParams {
  const sp = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value == null || value === '') continue
    sp.set(key, String(value))
  }
  return sp
}

/**
 * Appends a serialized query string to a base URL path.
 * Returns the path unchanged when there are no non-empty params.
 */
export function appendQuery(path: string, params: QueryParams): string {
  const qs = buildQuery(params).toString()
  return qs ? `${path}?${qs}` : path
}
