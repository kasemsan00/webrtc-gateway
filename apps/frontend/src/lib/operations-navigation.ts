export interface TrunkNavigationSource {
  trunkPublicId?: string
  trunkId?: number
}

export function trunkSearchFrom(source: TrunkNavigationSource) {
  const trunkPublicId = source.trunkPublicId?.trim()
  if (trunkPublicId) return { trunkPublicId }
  if (
    typeof source.trunkId === 'number' &&
    Number.isSafeInteger(source.trunkId) &&
    source.trunkId > 0
  ) {
    return { trunkId: source.trunkId }
  }
  return {}
}

export function instanceSearchFrom(instanceId: string | undefined) {
  const search = instanceId?.trim()
  return search ? { search } : {}
}
