export interface Trunk {
  id: number
  public_id?: string
  publicId?: string
  name: string
  domain: string
  port: number
  username: string
  transport: string
  enabled: boolean
  isDefault: boolean
  activeCallCount: number
  activeDestinations?: Array<string>
  leaseOwner: string
  leaseUntil: string
  lastRegisteredAt: string
  isRegistered: boolean
  lastError: string
  createdAt: string
  updatedAt: string
}

export interface UpdateTrunkPayload {
  name?: string
  domain?: string
  port?: number
  username?: string
  password?: string
  transport?: 'tcp' | 'udp'
  enabled?: boolean
  isDefault?: boolean
  updatedBy?: string
}

export interface CreateTrunkPayload {
  name: string
  domain: string
  port: number
  username: string
  password: string
  transport: 'tcp' | 'udp'
  enabled: boolean
  isDefault: boolean
}

export function normalizeTrunkUid(
  trunk: Pick<Trunk, 'public_id' | 'publicId'>,
) {
  return trunk.public_id ?? trunk.publicId ?? ''
}

export interface TrunkListResponse {
  items: Array<Trunk>
  total: number
  page: number
  pageSize: number
}

export interface TrunkListParams {
  page?: number
  pageSize?: number
  trunkId?: number
  trunkPublicId?: string
  search?: string
  createdAfter?: string
  createdBefore?: string
  sortBy?: string
  sortDir?: 'asc' | 'desc'
}
