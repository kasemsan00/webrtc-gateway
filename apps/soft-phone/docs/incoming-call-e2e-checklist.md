# Incoming Call E2E Checklist (`soft-phone` + `gateway-sip`)

## Preconditions
- `gateway-sip` running with reachable `/ws`.
- `soft-phone` connected to gateway and SIP registration successful.
- Two SIP endpoints available:
  - `soft-phone` callee.
  - External caller (SIP phone/PBX extension).

## Scenarios
1. Incoming notification path
- Trigger inbound SIP INVITE to `soft-phone`.
- Verify native incoming UI (CallKeep) appears.
- Verify in-app incoming modal appears when app is foregrounded.

2. Accept from in-app modal
- Tap `Answer` in app.
- Verify call transitions to `INCALL`.
- Verify two-way media (audio and video) is established.

3. Accept from native call UI
- Receive another inbound call.
- Accept from OS-native incoming UI.
- Verify app attaches to active call and shows in-call screen.

4. Decline flow
- Receive inbound call.
- Decline from app and from native UI in separate attempts.
- Verify SIP call is rejected and app returns to `IDLE`.
- Verify no stale `incomingCall` session remains in app state.

5. Multi-client race (first-accept-wins)
- Connect two clients to same gateway instance.
- Trigger one inbound call.
- Accept on client A first, then client B.
- Verify client A succeeds and client B receives rejection/error.

## Regression checks
- Outgoing call flow still works (offer -> answer -> call).
- DTMF and hangup still use active session ID.
- Resume/reconnect flow still resumes same active session.

