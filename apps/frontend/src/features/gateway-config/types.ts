export type GatewayConfigSections = Record<string, Record<string, unknown>>

export type GatewayConfigResponse = {
  instanceId: string
  source: string
  sections: GatewayConfigSections
}

export type ConfigValueType =
  | 'boolean'
  | 'number'
  | 'string'
  | 'secret'
  | 'empty'
  | 'other'

export type FlatConfigItem = {
  id: string
  subsystem: string
  key: string
  fullKey: string
  value: unknown
  type: ConfigValueType
}
