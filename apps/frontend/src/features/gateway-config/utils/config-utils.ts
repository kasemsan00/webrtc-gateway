import type {
  ConfigValueType,
  FlatConfigItem,
  GatewayConfigSections,
} from '../types'

export function detectValueType(value: unknown): ConfigValueType {
  if (value === null || value === undefined || value === '') return 'empty'
  if (value === '****') return 'secret'
  if (typeof value === 'boolean') return 'boolean'
  if (typeof value === 'number') return 'number'
  if (typeof value === 'string') return 'string'
  return 'other'
}

export function formatConfigValue(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

export function flattenConfigSections(
  sections: GatewayConfigSections | undefined | null,
): Array<FlatConfigItem> {
  if (!sections || typeof sections !== 'object') return []

  const items: Array<FlatConfigItem> = []

  function walk(
    subsystem: string,
    prefix: string,
    obj: Record<string, unknown>,
  ) {
    for (const [key, val] of Object.entries(obj)) {
      const path = prefix ? `${prefix}.${key}` : key
      if (
        val !== null &&
        typeof val === 'object' &&
        !Array.isArray(val)
      ) {
        walk(subsystem, path, val as Record<string, unknown>)
      } else {
        const fullKey = `${subsystem}.${path}`
        items.push({
          id: fullKey,
          subsystem,
          key: path,
          fullKey,
          value: val,
          type: detectValueType(val),
        })
      }
    }
  }

  for (const [subsystem, values] of Object.entries(sections)) {
    if (values && typeof values === 'object') {
      walk(subsystem, '', values)
    }
  }

  return items.sort((a, b) => a.fullKey.localeCompare(b.fullKey))
}

export function filterConfigItems(
  items: Array<FlatConfigItem>,
  search: string,
  selectedSubsystem: string,
): Array<FlatConfigItem> {
  const normalizedSearch = search.trim().toLowerCase()

  return items.filter((item) => {
    if (selectedSubsystem !== 'all' && item.subsystem !== selectedSubsystem) {
      return false
    }

    if (!normalizedSearch) return true

    if (item.fullKey.toLowerCase().includes(normalizedSearch)) return true
    if (item.key.toLowerCase().includes(normalizedSearch)) return true
    if (item.subsystem.toLowerCase().includes(normalizedSearch)) return true

    const valStr = formatConfigValue(item.value).toLowerCase()
    if (valStr.includes(normalizedSearch)) return true

    if (normalizedSearch === 'secret' || normalizedSearch === 'masked') {
      return item.type === 'secret'
    }
    if (normalizedSearch === 'empty' || normalizedSearch === 'null') {
      return item.type === 'empty'
    }
    if (normalizedSearch === 'bool' || normalizedSearch === 'boolean') {
      return item.type === 'boolean'
    }

    return false
  })
}

export function toEnvKey(fullKey: string): string {
  return fullKey
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replace(/[.-]/g, '_')
    .toUpperCase()
}

export function toEnvText(items: Array<FlatConfigItem>): string {
  return items
    .map((item) => {
      const key = toEnvKey(item.fullKey)
      const val =
        item.value === null || item.value === undefined
          ? ''
          : String(item.value)
      return `${key}=${val}`
    })
    .join('\n')
}

export function toJsonText(items: Array<FlatConfigItem>): string {
  const obj: Record<string, unknown> = {}
  for (const item of items) {
    obj[item.fullKey] = item.value
  }
  return JSON.stringify(obj, null, 2)
}

export function getSubsystemCounts(
  items: Array<FlatConfigItem>,
): Record<string, number> {
  const counts: Record<string, number> = {}
  for (const item of items) {
    counts[item.subsystem] = (counts[item.subsystem] ?? 0) + 1
  }
  return counts
}
