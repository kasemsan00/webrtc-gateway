# Incident Triage Workflow Design (Frontend + Gateway)

Date: 2026-03-15  
Status: Approved for planning  
Scope: `apps/frontend` + `apps/gateway`

## Problem

Operators need to identify failed-call causes faster. Current troubleshooting is fragmented across UI context and backend signals, increasing mean time to identify call failures.

## Goals

- Reduce mean time to identify call failures.
- Keep v1 lightweight with minimal new backend surface.
- Preserve existing call/media behavior (observability only).

## Non-Goals

- No SIP/media path behavior changes.
- No cross-call timelines or incident history system in v1.
- No broad logging UI explorer in v1.

## Chosen Approach

Use a frontend incident triage panel backed by one lightweight, read-only gateway diagnostics endpoint that returns normalized failure hints for a single call/session context.

## Architecture

- Frontend adds an **Incident Triage panel** on failed-call views.
- Panel requests diagnostics from a new gateway endpoint using known call/session identifiers.
- Gateway maps existing signaling/session outcomes into normalized diagnostic categories and recommended next checks.
- Frontend renders concise triage results and copy-ready handoff notes.

This is strictly additive observability; no protocol/media behavior changes.

## Components

### Frontend (`apps/frontend`)

- Failed-call UI entrypoint opens Incident Triage panel.
- Panel gathers context: `callId`, `attemptId` (if available), trunk identifier, timestamp.
- Calls gateway diagnostics endpoint.
- Renders:
  - failure category
  - summary
  - evidence bullets
  - suggested actions
  - operator note template (copy-ready)

### Gateway (`apps/gateway`)

- New read-only diagnostics handler (single-call lookup).
- Validates identifiers and resolves available call/session/signaling state.
- Maps raw outcomes into stable categories:
  - `signaling-timeout`
  - `auth-failure`
  - `trunk-unavailable`
  - `peer-disconnect`
  - `unknown` (explicit fallback category)
- Returns typed response payload for frontend rendering.

## Proposed API Contract (v1)

`GET /api/incidents/diagnostics?callId=<id>[&attemptId=<id>]`

Success response shape:

```json
{
  "category": "signaling-timeout",
  "summary": "INVITE was sent but no final SIP response was received within timeout.",
  "confidence": "medium",
  "evidence": [
    "Call session exists with outbound signaling start timestamp",
    "No terminal success/failure response recorded before timeout threshold"
  ],
  "suggested_actions": [
    "Check trunk reachability and SIP upstream health",
    "Verify target endpoint registration status"
  ],
  "operator_note_template": "Call {{callId}} failed due to signaling timeout. Checked trunk reachability and upstream health."
}
```

Error responses:

- `400` invalid/missing identifiers
- `404` call/session not found
- `500` diagnostics computation failure

## Data Flow

1. Operator opens failed call details in frontend.
2. Frontend sends diagnostics request with known call context.
3. Gateway validates input and reads relevant state.
4. Gateway computes normalized category + evidence + actions.
5. Frontend displays triage card and copy-ready note.

## Error Handling

- No silent failures.
- If diagnostics request fails, frontend shows explicit state:
  - “Diagnostics unavailable”
  - baseline manual checklist for continued triage
- Backend returns explicit typed errors; avoid broad catch-all masking.

## Testing Strategy

### Gateway tests

- Unit tests for category mapping logic.
- Edge cases:
  - missing session
  - stale/incomplete state
  - conflicting signals
  - fallback to `unknown`

### Frontend tests

- Rendering success response.
- Rendering typed backend errors.
- Rendering diagnostics-unavailable fallback.
- Handling malformed/unexpected payload safely.

### Contract test

- Integration-style test asserting diagnostics response schema stability between frontend expectations and gateway payload.

## Risks and Mitigations

- **Risk:** Incomplete runtime evidence yields low-confidence diagnosis.  
  **Mitigation:** Include confidence and explicit evidence in response.

- **Risk:** Operators over-trust automated hints.  
  **Mitigation:** Keep suggestions as guidance plus manual checklist fallback.

- **Risk:** Scope creep into log explorer.  
  **Mitigation:** Keep v1 to single-call diagnostics endpoint and triage panel.

## Implementation Boundaries (v1)

- In scope:
  - One diagnostics endpoint
  - One frontend triage panel in failed-call view
  - Category mapping + tests
- Out of scope:
  - Historical analytics
  - Multi-call incident correlation
  - Deep log query tooling

## Rollout Notes

- Feature can be shipped behind a frontend flag if needed.
- Start with internal operator use and collect feedback on category usefulness.
