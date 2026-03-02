# AGENTS.md - Softphone Mobile (React Native / Expo)

## Project Overview

React Native softphone app using Expo SDK 54, React 19.1, and React Native 0.81 with expo-router, Zustand state management, and WebRTC for SIP calls via a gateway server.

**Connection Model:** Single-use WebSocket connections - WebSocket connects only when user presses "เข้าใช้งาน" (Enter) button, makes one call, and disconnects automatically when call ends. No persistent connections or auto-reconnect on app startup.

**RTT Support:** Real-Time Text (RTT) over WebRTC DataChannel (primary) and SIP MESSAGE (fallback). Codec layer supports T.140 and RTT-XML (`urn:ietf:params:xml:ns:rtt`) with shared event model + reducer powering LiveText UI in the in-call screen.

## Build/Lint/Test Commands

```bash
# Development
npm start                    # Start Expo development server
expo start                   # Alternative

# Run on devices
npm run android              # expo run:android
npm run ios                  # expo run:ios
npm run ios:device           # expo run:ios --device
npm run web                  # expo start --web

# Building
npm run ios:build:local      # eas build --platform ios --local
npm run ios:build:device     # npx expo run:ios --device
npx expo prebuild --platform android  # Generates android/ native project
./android-libs/patch-android.bat      # Apply custom WebRTC software encoder patch (run after every prebuild)

# Linting
npm run lint                 # expo lint (ESLint with eslint-config-expo)

# Type Checking
npx tsc --noEmit             # TypeScript check without output

# Tests
npx jest <testfile>          # Example: npx jest __tests__/t140.test.ts
```

## Project Structure

```
app/                      # Expo Router screens (file-based routing)
  (tabs)/                 # Tab navigator group
    _layout.tsx           # Tab navigator layout
    index.tsx             # Main dialer screen
    settings.tsx          # Settings screen
  _layout.tsx             # Root layout
  modal.tsx               # Modal screen

components/
  ui/                     # Reusable UI primitives
    avatar.tsx            # Avatar component
    button.tsx            # Button component with CVA variants
    card.tsx              # Card container component
    collapsible.tsx       # Expandable/collapsible component
    icon.tsx              # Icon wrapper component
    input.tsx             # Text input component
    text.tsx              # Text component with variants
  softphone/              # Domain-specific components
    chat.tsx              # In-call chat/messaging UI
    dialpad.tsx           # Main dialpad for entering numbers
    dtmf-keypad.tsx       # In-call DTMF keypad overlay
    in-call-screen.tsx    # Active call UI with video, chat, DTMF
    live-text-composer.tsx # RTT local input UI
    live-text-viewer.tsx   # RTT remote display UI
    incoming-call-modal.tsx # Incoming call notification
    permission-error-modal.tsx # Permission error modal with retry logic
  ConnectionStatus.tsx    # Shows reconnection progress
  themed-text.tsx         # Theme-aware text component
  themed-view.tsx         # Theme-aware view component

hooks/                    # Custom React hooks
  use-color-scheme.ts     # Color scheme hook (native)
  use-color-scheme.web.ts # Color scheme hook (web)
  use-network-monitor.ts  # Network state hook
  use-theme-color.ts      # Theme color hook
  use-rtt.ts              # RTT integration hook (transport + decoder + reducer)

lib/                      # Core utilities and services
  gateway/                # WebSocket gateway client for SIP
    gateway-client.ts     # WebSocket/WebRTC client
    index.ts              # Exports
    types.ts              # Message types, ConnectionState, ReconnectConfig
  rtt/                    # Real-Time Text (RTT) layer
    rtt-events.ts         # Shared RTT event model and payload types
    rtt-xml.ts            # RTT-XML decoder/encoder
    t140.ts               # T.140 decoder/encoder
    encoder.ts            # Event batching + payload encoder
    transport.ts          # DataChannel + SIP MESSAGE transports
    rtt-reducer.ts        # Apply RTT events to state buffer
  network/                # Network monitoring for auto-reconnect
    index.ts              # Exports
    network-monitor.ts    # Network change detection service
    types.ts              # NetworkState, NetworkChangeEvent types
  sip/                    # SIP utilities
    index.ts              # SIP helper exports
  auto-connect.ts         # Auto-connection logic
  request-permissions.ts  # Permission request utilities

store/                    # Zustand stores
  dialer-store.ts         # Dialer draft number (persisted via MMKV)
  settings-store.ts       # App settings (persisted via MMKV)
  location-store.ts       # Selected location (in-memory, session-only; resets on app restart)
  sip-store.ts            # SIP state, calls, messages, DTMF

theme/                    # Centralized design tokens
  colors.ts               # Palette, semantic colors, light/dark, fonts
  spacing.ts              # Spacing scale, radii, re-exports scale helpers
  typography.ts           # Font sizes, weights, line heights
  index.ts                # Barrel export

styles/                   # Extracted StyleSheet definitions
  components/             # Styles for components/softphone/* & ConnectionStatus
    in-call-screen.styles.ts
    chat.styles.ts
    dialpad.styles.ts
    dtmf-keypad.styles.ts
    entry-form-view.styles.ts
    mode-selection-view.styles.ts
    permission-error-modal.styles.ts
    location-picker-modal.styles.ts
    live-text-composer.styles.ts
    live-text-viewer.styles.ts
    connection-status.styles.ts
  screens/                # Styles for app/ screens
    dialer.styles.ts
    settings.styles.ts
    root-layout.styles.ts

constants/                # App constants
  theme.ts                # Re-exports from theme/ (backward compat)

types/                    # TypeScript declaration files
  react-native-incall-manager.d.ts # InCall Manager types

assets/                   # Static assets (includes global CSS)
  images/                 # Image assets

docs/                     # Feature documentation (reconnect, bitrate, ICE fixes, etc.)

scripts/                  # Build/utility scripts
  reset-project.js        # Project reset script
android-libs/             # Native patch artifacts
  patch-android.bat       # Copies webrtc-release.aar & updates Gradle after prebuild
  webrtc-release.aar      # Custom WebRTC build with software encoder

android/                  # Native Android project (generated / used by `expo run:android`)
web/                      # Web entry + static assets
  app.js
  index.html
  style.css

# Root configs
app.json
eas.json
eslint.config.js
metro.config.js
package.json
tsconfig.json
react-native.md
web.md
```

## Code Style Guidelines

### TypeScript

- **Strict mode enabled** (`"strict": true` in tsconfig.json)
- **Path aliases**: Use `@/` for root imports (e.g., `@/components/ui/button`)
- **Never use**: `as any`, `@ts-ignore`, `@ts-expect-error`
- **Explicit types** for function parameters and return types on public APIs
- **Interfaces** for object shapes, **type** for unions/intersections

```typescript
// GOOD
interface UseSipCallReturn {
  isRegistered: boolean;
  call: (number: string) => Promise<void>;
}

// GOOD - Type for unions
type CallState = "idle" | "calling" | "ringing" | "incall";

// BAD - No 'any'
const handler = (data: any) => {}; // Never do this
```

### Imports

Order imports as follows (separated by blank lines):

1. React/React Native
2. External packages (expo, navigation, etc.)
3. Internal absolute imports (`@/`)
4. Relative imports

```typescript
import React, { useCallback, useEffect, useState } from "react";
import { View, Pressable, StyleSheet } from "react-native";

import { Stack } from "expo-router";
import { create } from "zustand";

import { useSipStore } from "@/store/sip-store";
import { cn } from "@/lib/utils";

import { Text } from "./text";
```

### Naming Conventions

| Type               | Convention                          | Example              |
| ------------------ | ----------------------------------- | -------------------- |
| Files (components) | kebab-case                          | `in-call-screen.tsx` |
| Files (hooks)      | kebab-case with `use-` prefix       | `use-sip-call.ts`    |
| Components         | PascalCase                          | `InCallScreen`       |
| Hooks              | camelCase with `use` prefix         | `useSipCall`         |
| Zustand stores     | camelCase with `use` prefix         | `useSipStore`        |
| Interfaces         | PascalCase                          | `SipConfig`          |
| Enums              | PascalCase (values SCREAMING_SNAKE) | `CallState.INCALL`   |
| Constants          | SCREAMING_SNAKE_CASE                | `MAX_RETRY_COUNT`    |

### React Native Components

- Use functional components with hooks
- Use `StyleSheet.create()` for styles (not inline objects)
- Use `react-native-safe-area-context` for safe areas
- Prefer `Pressable` over `TouchableOpacity` for new components

```typescript
export function MyComponent({ onPress }: Props) {
  return (
    <SafeAreaView style={styles.container}>
      <Pressable onPress={onPress} style={styles.button}>
        <Text>Press Me</Text>
      </Pressable>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  button: { padding: 16 },
});
```

### State Management (Zustand)

- Single store per domain (`sip-store.ts`, `settings-store.ts`, `dialer-store.ts`, `location-store.ts`)
- Separate state interface from actions interface
- Use selectors to avoid unnecessary re-renders
- Prefix internal methods with underscore

Dialer draft input is persisted to MMKV via `store/dialer-store.ts` so the **last typed number restores after app restart** and is **not auto-cleared on hangup/call events** (it only changes when the user edits it).

Location selection in `store/location-store.ts` is intentionally **not persisted**. It is session-only runtime state: users can override location during the current app session, and on full app restart the app falls back to fetching current geolocation again.

```typescript
interface SipState {
  isConnected: boolean;
  callState: CallState;
}

interface SipActions {
  connect: () => Promise<void>;
  _startCallTimer: () => void; // Internal method
}

type SipStore = SipState & SipActions;

// Usage - select specific values
const isConnected = useSipStore((s) => s.isConnected);
const callState = useSipStore((s) => s.callState);
```

### Error Handling

- Always wrap async operations in try/catch
- Log errors with context prefix: `console.error("[Module] Error:", error)`
- Create safe wrappers for native modules that may fail

```typescript
const safeNativeCall = {
  start: () => {
    try {
      NativeModule?.start?.();
    } catch (e) {
      console.warn("[SafeWrapper] start failed:", e);
    }
  },
};

// Async error handling
const handleCall = useCallback(async () => {
  try {
    await call(phoneNumber);
  } catch (error) {
    console.error("[DialerScreen] Failed to make call:", error);
  }
}, [phoneNumber, call]);
```

### UI Components (CVA Pattern)

Use `class-variance-authority` for component variants:

```typescript
import { cva, type VariantProps } from "class-variance-authority";

const buttonVariants = cva("flex-row items-center justify-center", {
  variants: {
    variant: {
      default: "bg-primary",
      destructive: "bg-destructive",
    },
    size: {
      default: "h-12 px-6",
      sm: "h-9 px-3",
    },
  },
  defaultVariants: {
    variant: "default",
    size: "default",
  },
});
```

### Styling

- Use `cn()` helper from `@/lib/utils` for conditional classes
- Keep StyleSheet for complex/dynamic styles

```typescript
import { cn } from "@/lib/utils";

<View className={cn("flex-1 bg-background", isActive && "bg-primary")} />;
```

## Key Dependencies

| Package                         | Purpose                   |
| ------------------------------- | ------------------------- |
| expo-router                     | File-based navigation     |
| zustand                         | State management          |
| react-native-webrtc             | WebRTC for calls          |
| react-native-incall-manager     | In-call audio routing     |
| react-native-mmkv               | Fast persistent storage   |
| @react-native-community/netinfo | Network state monitoring  |
| lucide-react-native             | Icons                     |
| class-variance-authority        | Component variants        |
| expo-haptics                    | Haptic feedback           |
| expo-image                      | Optimized image component |
| react-native-gesture-handler    | Gesture handling          |
| react-native-reanimated         | Animations                |
| react-native-svg                | SVG support               |
| @rn-primitives/slot             | Primitive slot component  |

## Common Patterns

### Hooks Pattern

Hooks should return an object with clear state/action separation:

```typescript
export function useSipCall(): UseSipCallReturn {
  const [state, setState] = useState<CallState>(CallState.IDLE);

  const call = useCallback(async (number: string) => {
    // implementation
  }, []);

  return { state, call };
}
```

### Public Entry Form (Main Screen)

The main dialer screen (`app/(tabs)/index.tsx`) now starts with a mode selection (Normal/Emergency) before showing the form.

**Flow:**

1. User selects a mode (Normal/Emergency) with animated buttons.
2. Form appears for `fullName`, `phone`, and `agency`.
3. User submits the form (`เข้าใช้งาน`).
4. App sends a POST to `/extension/public` with form data and `emergency` flag.
5. Response provides SIP credentials (`domain`, `ext`, `secret`).
6. App updates settings, connects WebSocket, registers SIP, and starts an outgoing call to `9999`.

**Validation:**

- `fullName` and `agency` are required.
- `phone` must be 9–10 digits.

**Components:**

- `ModeSelectionView` – Animated mode buttons (scale 0.95 → 1.0 on press) using `react-native-reanimated`.
- `EntryFormView` – Form inputs with validation and submit/back actions.
- `useEntrySubmit` – Hook encapsulating API/SIP submission logic.

### WebSocket Message Types

Gateway messages use discriminated unions with `type` field:

```typescript
type OutgoingMessage =
  | { type: "register"; sipUsername: string; ... }
  | { type: "call"; destination: string; ... }
  | { type: "hangup"; }
  | { type: "dtmf"; sessionId: string; digits: string; }
  | { type: "send_message"; body: string; contentType?: string; };
```

## Gateway Features

### DTMF Support

Send DTMF tones during active calls:

```typescript
// In gateway-client.ts
sendDtmf(digit: string): void {
  this.send({
    type: "dtmf",
    sessionId: this.sessionId,
    digits: digit,
  });
}

// Usage via store
const sendDtmf = useSipStore((s) => s.sendDtmf);
sendDtmf("5"); // Send digit 5
```

The `DtmfKeypad` component provides a UI overlay for in-call DTMF input with haptic feedback.

### SIP MESSAGE (In-call Chat)

Send and receive text messages during calls:

```typescript
// Send message
const sendMessage = useSipStore((s) => s.sendMessage);
sendMessage("Hello!");

// Read messages
const messages = useSipStore((s) => s.messages);
const unreadCount = useSipStore((s) => s.unreadMessageCount);
```

Messages are stored in-memory and cleared when the call ends.

### Connection State & Reconnection

Automatic reconnection with exponential backoff:

```typescript
// Connection states
type ConnectionState = "disconnected" | "connecting" | "connected" | "reconnecting";

// Configure reconnection
const setReconnectConfig = useSipStore((s) => s.setReconnectConfig);
setReconnectConfig({
  maxAttempts: 5, // Give up after 5 attempts
  baseDelay: 1000, // Start with 1s delay
  maxDelay: 30000, // Cap at 30s
  backoffMultiplier: 2, // Double delay each attempt
});

// Manual reconnect (resets attempt counter)
const manualReconnect = useSipStore((s) => s.manualReconnect);
await manualReconnect();
```

The `ConnectionStatus` component shows reconnection progress with tap-to-retry.

### Network Auto-Reconnect

Proactive reconnection when network/IP changes (e.g., WiFi to 5G switch):

```typescript
// Network monitor detects IP change and triggers reconnection
import { getNetworkMonitor } from "@/lib/network";

const networkMonitor = getNetworkMonitor();
await networkMonitor.initialize({ debounceMs: 500 });

// Listen for network changes
networkMonitor.addListener((event) => {
  // event.type: 'ip_changed' | 'connection_lost' | 'connection_restored'
  console.log("Network changed:", event.type);
});

// Use the React hook in components
import { useNetworkMonitor } from "@/hooks/use-network-monitor";

const { isConnected, ipAddress, networkType } = useNetworkMonitor();
```

Call resumption after network change:

1. Saves call state (sessionId, remoteNumber) before disconnect
2. Force closes WebSocket immediately (no 30-60s timeout wait)
3. Reconnects and re-registers with SIP server
4. Sends `resume` message to K2 Gateway with saved sessionId
5. Falls back to automatic callback if resume fails

See `docs/network-auto-reconnect.md` for full documentation.

### Video Quality Profiles

Dynamic video resolution and bitrate configuration with automatic profile matching:

**Available Profiles** (defined in `store/settings-store.ts`):

| Resolution | Dimensions | Bitrate | Frame Rate | Use Case |
|-----------|-----------|---------|-----------|----------|
| **360p** | 640x360 | 1 Mbps | 30fps | Low bandwidth / mobile data |
| **480p** | 854x480 | 1.5 Mbps | 30fps | Balanced quality/bandwidth |
| **720p** ⭐ | 1280x720 | 3 Mbps | 30fps | **Default** - HD quality |
| **1080p** | 1920x1080 | 5 Mbps | 30fps | High quality / WiFi |

**Implementation:**

```typescript
// In settings-store.ts
export const BITRATE_MAP: Record<VideoResolution, number> = {
  "1080": 5000, // 5 Mbps
  "720": 3000,  // 3 Mbps (default)
  "480": 1500,  // 1.5 Mbps
  "360": 1000,  // 1 Mbps
};

// Get video constraints for getUserMedia
import { getVideoConstraints, getBitrateForResolution } from "@/store/settings-store";

const constraints = getVideoConstraints(); // Returns MediaTrackConstraints based on current settings
const bitrate = getBitrateForResolution("720"); // Returns 3000 (kbps)
```

**How it works:**

1. User selects resolution in Settings UI → automatically saved to MMKV
2. When making/receiving calls, `gateway-client.ts` reads settings and applies:
   - **getUserMedia constraints**: resolution + frame rate from `getVideoConstraints()`
   - **Outgoing bitrate limit**: via `RTCRtpSender.setParameters()` with `maxBitrate`
   - **Incoming bitrate limit**: via SDP `b=AS:` parameter manipulation
3. Bitrate is automatically matched to resolution profile (no manual configuration needed)

**Settings UI** (`app/(tabs)/settings.tsx`):

- Shows all 4 resolution options with bitrate labels
- Tap to change → saved immediately to MMKV
- Changes apply on next call (requires new getUserMedia)

**Files modified:**
- `store/settings-store.ts` - Added `BITRATE_MAP`, `getBitrateForResolution()`, `getVideoConstraints()`
- `lib/gateway/gateway-client.ts` - Uses dynamic settings instead of hardcoded values
- `app/(tabs)/settings.tsx` - Shows bitrate info in resolution picker

### Permission Handling & Video-Only Mode

**All calls require both camera and microphone permissions** - audio-only fallback has been removed.

**Permission Error Flow:**

```typescript
// Max retry attempts
const MAX_PERMISSION_RETRIES = 3;

// Permission state (in sip-store.ts)
interface SipState {
  permissionError: string | null;
  permissionRetryCount: number;
  missingPermissions: Array<"camera" | "microphone">;
}

// Actions
setPermissionError(error: string | null, missingPerms?: Array<"camera" | "microphone">): void
incrementPermissionRetry(): void
resetPermissionRetry(): void
```

**User Experience:**

1. **Call attempt** → Request camera + microphone permissions
2. **If denied** → Show `PermissionErrorModal` with:
   - Clear message indicating which permissions are missing
   - **"Open Settings"** button - Deep links to app settings (iOS/Android)
   - **"Retry"** button - Allows up to 3 retry attempts
   - **"Cancel"** button - Cancels call and resets retry count
3. **After 3 failed attempts** → Only "Open Settings" and "Cancel" remain
4. **On app restart** → Retry count resets to 0

**Implementation Details:**

- `getLocalMedia()` always requests both audio and video
- Throws clear error messages when permissions denied
- Dialer screen catches permission errors and displays modal
- Modal supports deep linking via `Linking.openSettings()` with platform-specific fallbacks
- Dark theme matching app design (`#0F172A` background)

**Settings Migration:**

Legacy `videoEnabled` setting is automatically removed from MMKV storage on app launch.

```typescript
// In settings-store.ts onRehydrateStorage
if (state.videoEnabled !== undefined) {
  console.log("[SettingsStore] Removing legacy videoEnabled field (audio-only mode removed)");
  delete state.videoEnabled;
}
```

**Files involved:**
- `lib/gateway/gateway-client.ts` - Always requests video in `getLocalMedia()`
- `store/sip-store.ts` - Permission error state tracking
- `store/settings-store.ts` - Removed `videoEnabled`, added migration
- `components/softphone/permission-error-modal.tsx` - Permission error UI
- `app/(tabs)/index.tsx` - Permission error handling and modal integration

## Do NOT

- Use `as any` or type assertions to silence errors
- Commit `.env` files or credentials
- Use inline styles for static values
- Create circular dependencies between stores
- Implement audio-only mode or fallback (removed - video+audio only)
- Allow calls without camera permission (blocked by permission error modal)
