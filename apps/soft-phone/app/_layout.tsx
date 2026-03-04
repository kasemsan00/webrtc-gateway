import {
  DarkTheme,
  DefaultTheme,
  ThemeProvider,
} from '@react-navigation/native';
import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect } from 'react';
import { AppState, AppStateStatus } from 'react-native';
import 'react-native-reanimated';
import '../assets/global.css';

import { CallOverlays } from '@/components/softphone/call-overlays';
import { useColorScheme } from '@/hooks/use-color-scheme';
import { initializeAutoConnect } from '@/lib/auto-connect';
import {
  registerCallKeepListeners,
  removeCallKeepListeners,
  setupCallKeep,
} from '@/lib/callkeep';
import { getNetworkMonitor, resetNetworkMonitor } from '@/lib/network';
import { requestMediaPermissions } from '@/lib/request-permissions';
import { useSipStore } from '@/store/sip-store';
import { Colors } from '@/theme';

export const unstable_settings = {
  anchor: '(tabs)',
};

const AppDarkTheme = {
  ...DarkTheme,
  colors: {
    ...DarkTheme.colors,
    background: Colors.dark.background,
    card: Colors.dark.surface,
    border: Colors.dark.border,
    text: Colors.dark.text,
  },
};

const AppLightTheme = {
  ...DefaultTheme,
  colors: {
    ...DefaultTheme.colors,
    background: Colors.light.background,
    card: Colors.light.surface,
    border: Colors.light.border,
    text: Colors.light.text,
  },
};

export default function RootLayout() {
  const colorScheme = useColorScheme();
  const setupNetworkMonitor = useSipStore((state) => state.setupNetworkMonitor);

  // Handle background/foreground transitions for active calls
  useEffect(() => {
    const appStateRef = { current: AppState.currentState };

    const subscription = AppState.addEventListener(
      'change',
      (nextAppState: AppStateStatus) => {
        const prevState = appStateRef.current;
        appStateRef.current = nextAppState;
        console.log(
          '[RootLayout] AppState changed:',
          prevState,
          '->',
          nextAppState,
        );

        if (
          prevState === 'active' &&
          (nextAppState === 'inactive' || nextAppState === 'background')
        ) {
          // App moving to background; PiP is handled by RTCView iosPIP settings
        } else if (
          (prevState === 'inactive' || prevState === 'background') &&
          nextAppState === 'active'
        ) {
          // App returning to foreground
          void useSipStore
            .getState()
            .handleAppForegroundRecovery()
            .catch((error) => {
              console.error('[RootLayout] Foreground recovery failed:', error);
            });
        }
      },
    );

    return () => {
      subscription.remove();
    };
  }, []);

  // Request permissions and initialize auto-connect on app startup
  useEffect(() => {
    const initializeApp = async () => {
      try {
        console.log('🚀 App started - requesting permissions...');

        // Request camera and microphone permissions first
        const permissions = await requestMediaPermissions();
        console.log('📋 Permission results:', permissions);

        // Initialize CallKeep for native call integration
        console.log('📞 Initializing CallKeep...');
        await setupCallKeep();
        registerCallKeepListeners({
          answer: () => useSipStore.getState().answer(),
          hangup: () => useSipStore.getState().hangup(),
          toggleMute: (muted: boolean) => {
            const currentMuted = useSipStore.getState().isMuted;
            if (currentMuted !== muted) {
              useSipStore.getState().toggleMute();
            }
          },
          sendDtmf: (digits: string) => useSipStore.getState().sendDtmf(digits),
          onAudioSessionActivated: () => {
            useSipStore.getState().refreshRemoteVideo('callkeep_audio_session');
            void useSipStore
              .getState()
              .handleAppForegroundRecovery()
              .catch((error) => {
                console.error(
                  '[RootLayout] Audio session foreground recovery failed:',
                  error,
                );
              });
          },
        });

        // Initialize network monitor for auto-reconnect on network change
        console.log('📶 Initializing network monitor...');
        const networkMonitor = getNetworkMonitor();
        await networkMonitor.initialize({
          debounceMs: 500,
          enableLogging: __DEV__,
        });

        // Setup SIP store handlers for network reconnection
        setupNetworkMonitor();

        // Then initialize auto-connect
        console.log('🔌 Initializing auto-connect...');
        initializeAutoConnect();
      } catch (error) {
        console.error('[RootLayout] initializeApp failed:', error);
      }
    };

    initializeApp();

    // Cleanup network monitor and CallKeep on unmount
    return () => {
      resetNetworkMonitor();
      removeCallKeepListeners();
    };
  }, [setupNetworkMonitor]);

  return (
    <ThemeProvider
      value={colorScheme === 'dark' ? AppDarkTheme : AppLightTheme}
    >
      <Stack
        screenOptions={{
          contentStyle: {
            backgroundColor:
              colorScheme === 'dark'
                ? Colors.dark.background
                : Colors.light.background,
          },
        }}
      >
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen
          name="modal"
          options={{ presentation: 'modal', title: 'Modal' }}
        />
      </Stack>
      <StatusBar style={colorScheme === 'dark' ? 'light' : 'dark'} />
      <CallOverlays />
    </ThemeProvider>
  );
}
