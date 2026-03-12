function normalizeEnvValue(raw: unknown): string | undefined {
  if (typeof raw !== 'string') return undefined
  const value = raw.trim()
  return value || undefined
}

function readWindowRuntimeEnv(name: keyof ImportMetaEnv): string | undefined {
  if (typeof window === 'undefined') return undefined

  const runtimeEnv = (
    window as Window & {
      __APP_RUNTIME_ENV__?: Record<string, unknown>
    }
  ).__APP_RUNTIME_ENV__

  if (!runtimeEnv) return undefined
  return normalizeEnvValue(runtimeEnv[name])
}

export function readAppEnvValue(name: keyof ImportMetaEnv): string | undefined {
  const runtimeValue = readWindowRuntimeEnv(name)
  if (runtimeValue) return runtimeValue
  return normalizeEnvValue(import.meta.env[name])
}
