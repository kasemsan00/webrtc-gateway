# Gateway Troubleshooting Quick Guide

---

## 488 Not Acceptable Here

Most common causes:

1. malformed SDP `o=` username
2. codec mismatch (must support Opus + H.264)
3. video not enabled on SIP endpoint

Checkpoints:

- confirm SDP origin username fallback logic in `internal/sip/sdp.go`
- confirm peer config has `allow=opus` and `allow=h264`
- confirm `videosupport=yes`/equivalent endpoint settings

## Video rejected (`m=video ... 0`)

- inspect full SDP answer logs through `https://k2-gateway.kasemsan.com/api/logs/current?tail=500`
- validate AVPF compatibility (`SIP_VIDEO_USE_AVPF` and endpoint support)
- temporarily force AVP with `SIP_FORCE_AVP=true` for interoperability testing

## Auth/register timeouts

- transport consistency matters; requests explicitly set transport to avoid digest retry switching transports.

## 401 Unauthorized on API/WS

Checkpoints:

- Verify `AUTH_ENABLE`, `AUTH_JWKS_URL`, `AUTH_JWT_ISSUER`, `AUTH_JWT_AUDIENCE` are set correctly.
- Verify token `iss` and `aud` match configured values exactly.
- Verify token is not expired (`exp`) and is already valid (`nbf`).
- Verify JWT header `kid` exists in current JWKS (gateway auto-refreshes JWKS once on unknown `kid`).
- For WebSocket, verify client sends `access_token` query param on the connect URL.
