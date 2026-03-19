import { readAppEnvValue } from '@/lib/runtime-env'

export interface KeycloakRuntimeConfig {
  url: string
  realm: string
  clientId: string
}

function readEnvValue(name: keyof ImportMetaEnv): string | undefined {
  return readAppEnvValue(name)
}

function requireEnvValue(name: keyof ImportMetaEnv): string {
  const value = readEnvValue(name)
  if (!value) {
    throw new Error(`${name} is not configured`)
  }
  return value
}

export function getKeycloakRuntimeConfig(): KeycloakRuntimeConfig {
  return {
    url: requireEnvValue('VITE_KEYCLOAK_URL'),
    realm: requireEnvValue('VITE_KEYCLOAK_REALM'),
    clientId: requireEnvValue('VITE_KEYCLOAK_CLIENT'),
  }
}

export function isAutoRecordEnabled(): boolean {
  const value = readEnvValue('VITE_CONFIG_AUTORECORD')
  if (!value) return false
  return ['1', 'true', 'yes', 'on'].includes(value.toLowerCase())
}
