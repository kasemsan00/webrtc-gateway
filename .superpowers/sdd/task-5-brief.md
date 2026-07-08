### Task 5: Frontend — WS clients types + API service

**Files:**
- Create: `apps/frontend/src/features/ws-clients/types.ts`
- Create: `apps/frontend/src/features/ws-clients/services/ws-clients-api.ts`
- Create: `apps/frontend/src/features/ws-clients/services/ws-clients-api.test.ts`

**Interfaces:**
- Consumes: `fetchJson`, `resolveGatewayApiBaseUrl` from `@/lib/http-client`; `subscribeAuthenticatedSse` from `@/lib/sse-subscriber`
- Produces:
  - `fetchWSClients(): Promise<Array<WSClient>>`
  - `subscribeWSClientEvents(onEvent, onError?): () => void`

- [ ] **Step 1: Create types**

```typescript
// apps/frontend/src/features/ws-clients/types.ts
export interface WSClient {
  clientId: string
  sessionId?: string
  connectedAt: string
  trunkResolved: boolean
  resolvedTrunkId?: number
  resolvedTrunkPublicId?: string
  availability?: string
  callState?: string
  authSubject?: string
  publicOnly?: boolean
}

export interface WSClientStreamEvent {
  type: string
  clientId: string
  at: string
  client?: WSClient
}
```

- [ ] **Step 2: Write failing test**

```typescript
// apps/frontend/src/features/ws-clients/services/ws-clients-api.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchWSClients } from './ws-clients-api'

describe('ws-clients-api', () => {
  afterEach(() => vi.restoreAllMocks())

  it('fetches ws clients list', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => [{ clientId: 'c1', trunkResolved: true, resolvedTrunkId: 5 }],
    })
    vi.stubGlobal('fetch', fetchMock)
    const result = await fetchWSClients()
    expect(result).toHaveLength(1)
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/ws-clients')
  })
})
```

- [ ] **Step 3: Implement API service**

```typescript
// apps/frontend/src/features/ws-clients/services/ws-clients-api.ts
import type { WSClient, WSClientStreamEvent } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { subscribeAuthenticatedSse } from '@/lib/sse-subscriber'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchWSClients(): Promise<Array<WSClient>> {
  return fetchJson<Array<WSClient>>(`${API_BASE}/ws-clients`)
}

export function subscribeWSClientEvents(
  onEvent: (event: WSClientStreamEvent) => void,
  onError?: (event: Event) => void,
) {
  return subscribeAuthenticatedSse<WSClientStreamEvent>({
    url: `${API_BASE}/ws-clients/stream`,
    eventName: 'ws-client',
    onEvent,
    onError,
  })
}
```

- [ ] **Step 4: Run tests**

Run: `pnpm --filter frontend run test -- src/features/ws-clients/services/ws-clients-api.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/frontend/src/features/ws-clients/
git commit -m "feat(frontend): add ws-clients API service and types"
```
