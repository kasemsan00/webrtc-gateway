# AGENTS.md - softphone-mobile

## Overview
Expo Router React Native softphone client for gateway SIP/WebRTC calls.

## Project Structure
- Routes/screens: `app/` and `app/(tabs)/` (`index.tsx`, `history.tsx`, `settings.tsx`)
- UI components: `components/`, `components/ui/`, `components/softphone/`
- Domain logic: `lib/gateway/`, `lib/network/`, `lib/sip/`, `lib/vrs/`
- Stores: `store/` (`sip-store.ts`, `dialer-store.ts`, `settings-store.ts`, `call-history-store.ts`)
- Hooks: `hooks/`
- Styles: `styles/components/`, `styles/screens/`
- Build scripts/artifacts: `scripts/`
- Generated native Android tree: `android/` (avoid manual edits unless task explicitly requires native changes)

## Commands
Run in `E:\dev\webrtc\softphone-mobile`.

- Install: `npm install`
- Dev server: `npm start` (or `expo start`)
- Android prebuild: `npm run android:prebuild`
- Android run: `npm run android`
- iOS run: `npm run ios`
- iOS device run: `npm run ios:device`
- Web run: `npm run web`
- Android cloud build: `npm run android:build:online`
- iOS local build: `npm run ios:build:local`
- iOS device build/run: `npm run ios:build:device`
- Lint: `npm run lint`
- Type check: `npx tsc --noEmit`

Notes:
- `package.json` currently has `reset-project` script, but `scripts/reset-project.js` is not present.
- No `__tests__/` directory currently exists in this project.

## Conventions
- TypeScript strict mode enabled.
- Use `@/*` path alias.
- Import order: React/React Native, external packages, `@/`, relative.
- Avoid `any`, `@ts-ignore`, and `@ts-expect-error` unless clearly justified.
- Prefer functional components + hooks and `Pressable` for new interactions.
- Keep styles in `StyleSheet.create()` or dedicated style modules.
- Use store selectors to reduce unnecessary re-renders.

## Contract Notes
- Keep gateway WebSocket/API payload compatibility with `webrtc-gateway-sip`.
- If call/session/trunk/message payloads change, update this app and other clients in the same change set.
