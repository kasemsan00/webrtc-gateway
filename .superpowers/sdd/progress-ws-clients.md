# SDD progress: ws-clients-realtime

Plan: docs/superpowers/plans/2026-07-08-ws-clients-realtime.md
Branch base: 54739ae

Pre-flight resolutions:
- Task 3: refactor handleListWSCients to use buildWSClientResponse (DRY)
- Task 6: remove useVisibilityRealtimeReload, single SSE useEffect only

Task 1: complete (54739ae..3245797, review clean; 2 Minor: test coverage gap + lock discipline)
Minor findings for final review:
1. handleListWSClients test dropped coverage of trunkManager→ResolvedTrunkPublicID and authClaims→AuthSubject branches (production code retains them)
2. New test writes wsConnections without srv.mu (matches brief, safe in single-goroutine test, diverges from file convention)

Task 2: complete (3242797..2ba1e00, review clean, no findings)
Task 3: complete (2ba1e00..d5dcbc4, review clean; 2 Minor → 1 Important for Task 4: notifyWSClientChanged reads client fields without s.mu.RLock — must add lock in Task 4)
Task 4: complete (d5dcbc4..8e71d0c, review clean; 1 Important pre-existing (I1: unlocked writes at ws_trunk.go:232/ws_call.go:311 — per-brief), 3 Minor (M1: oldClient not notified on resume; M2: pre-existing test stub races; M3: Windows race toolchain))

Minor/Important findings for final review:
1. [Task1] handleListWSClients test dropped coverage of trunkManager→ResolvedTrunkPublicID and authClaims→AuthSubject branches
2. [Task1] New test writes wsConnections without srv.mu (safe in single-goroutine test)
3. [Task4-I1] Pre-existing unlocked writes to trunkResolved/resolvedTrunkID at ws_trunk.go:232-233, ws_call.go:311-312 — RLock protects read but not write side
4. [Task4-M1] oldClient.sessionID="" at ws_resume.go:211 not notified on resume-swap
5. [Task4-M2] Pre-existing test-stub races in stubSIPCallMaker/incomingTestSIPCallMaker (5 tests fail under -race, pre-existing)
Task 5: complete (8e71d0c..c0a6bbf, review clean; 5 Minor all informational)
Task 6: complete (c0a6bbf..1ebb1eb, review clean; 2 Minor informational)
Task 7: complete (1ebb1eb..78c315c, review clean; 2 Minor pre-existing/hygiene)
Task 8: complete (78c315c..c93fa36, review clean; no findings)

ALL TASKS COMPLETE. Branch: 54739ae..f40b7d2 (7 implementation commits + 1 docs commit + 1 fix commit)
Final review: 2 Important findings fixed and re-reviewed clean.
Ready for finishing-a-development-branch.
