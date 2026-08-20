import { readAppEnvValue } from '@/lib/runtime-env'

function readEnvValue(name: keyof ImportMetaEnv): string | undefined {
  return readAppEnvValue(name)
}

export function isAutoRecordEnabled(): boolean {
  const value = readEnvValue('VITE_CONFIG_AUTORECORD')
  if (!value) return false
  return ['1', 'true', 'yes', 'on'].includes(value.toLowerCase())
}
