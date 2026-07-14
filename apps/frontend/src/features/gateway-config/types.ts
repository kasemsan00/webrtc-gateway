export type GatewayConfigSections = Record<string, Record<string, unknown>>

export type GatewayConfigResponse = {
  instanceId: string
  source: string
  sections: GatewayConfigSections
}
