import { DarkTheme, DefaultTheme, ThemeProvider } from "@react-navigation/native";
import { Image } from "expo-image";
import * as Location from "expo-location";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect, useMemo, useState } from "react";
import { AppState, AppStateStatus, LogBox, Platform, Pressable, View } from "react-native";
import Animated, { FadeInUp, FadeOutUp } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import "../assets/global.css";

import { LocationPickerModal } from "@/components/softphone/location-picker-modal";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { useColorScheme } from "@/hooks/use-color-scheme";
import { registerCallKeepListeners, removeCallKeepListeners, setupCallKeep } from "@/lib/callkeep";
import { getNetworkMonitor, resetNetworkMonitor } from "@/lib/network";
import { requestMediaPermissions } from "@/lib/request-permissions";
import { useEntryStore } from "@/store/entry-store";
import { useLocationStore } from "@/store/location-store";
import { useSipStore } from "@/store/sip-store";
import { styles } from "@/styles/screens/root-layout.styles";

export const unstable_settings = {
  anchor: "(tabs)",
};

if (__DEV__) {
  // Expo dev wrapper may try to enable keep-awake before an Activity is ready on Android.
  // This is non-fatal but appears as an uncaught promise error in Metro logs.
  LogBox.ignoreLogs(["Unable to activate keep awake"]);
}

export default function RootLayout() {
  const colorScheme = useColorScheme();
  const entryMode = useEntryStore((s) => s.entryMode);
  const setupNetworkMonitor = useSipStore((state) => state.setupNetworkMonitor);
  const handleAppForegroundRecovery = useSipStore((state) => state.handleAppForegroundRecovery);
  const insets = useSafeAreaInsets();

  const currentLocation = useLocationStore((s) => s.currentLocation);
  const setLocation = useLocationStore((s) => s.setLocation);

  const [locationLabel, setLocationLabel] = useState<string>("Fetching location...");
  const [locationError, setLocationError] = useState<string | null>(null);
  const [showLocationPicker, setShowLocationPicker] = useState(false);
  const [locationBarHeight, setLocationBarHeight] = useState(0);

  const locationText = useMemo(() => {
    if (currentLocation) return currentLocation.address;
    if (locationError) return locationError;
    return locationLabel;
  }, [currentLocation, locationError, locationLabel]);

  // Request permissions and initialize network monitor on app startup
  // Note: WebSocket connection now happens only when user presses "เข้าใช้งาน" button
  useEffect(() => {
    const initializeApp = async () => {
      console.log("🚀 App started - requesting permissions...");

      // Request camera and microphone permissions first
      const permissions = await requestMediaPermissions();
      console.log("📋 Permission results:", permissions);

      // Initialize CallKeep for native call integration
      console.log("📞 Initializing CallKeep...");
      await setupCallKeep();
      registerCallKeepListeners({
        hangup: () => {
          void useSipStore
            .getState()
            .hangup()
            .catch((error) => {
              console.error("[RootLayout] CallKeep hangup failed:", error);
            });
        },
        toggleMute: (muted: boolean) => {
          const currentMuted = useSipStore.getState().isMuted;
          if (currentMuted !== muted) {
            useSipStore.getState().toggleMute();
          }
        },
        sendDtmf: (digits: string) => useSipStore.getState().sendDtmf(digits),
        onAudioSessionActivated: () => {
          useSipStore.getState().refreshRemoteVideo("callkeep_audio_session");
          void useSipStore
            .getState()
            .handleAppForegroundRecovery()
            .catch((error) => {
              console.error("[RootLayout] Audio session recovery failed:", error);
            });
        },
      });

      setLocationError(null);
      setLocationLabel("Fetching location...");

      try {
        const permissionStatus = await Location.getForegroundPermissionsAsync();
        const status = permissionStatus.status === "granted" ? permissionStatus : await Location.requestForegroundPermissionsAsync();

        if (status.status !== "granted") {
          setLocationError("Location permission denied");
        } else {
          const position = await Location.getCurrentPositionAsync({
            accuracy: Location.Accuracy.Balanced,
          });

          const places = await Location.reverseGeocodeAsync({
            latitude: position.coords.latitude,
            longitude: position.coords.longitude,
          });

          const place = places[0];
          const addressParts = [place?.name, place?.street, place?.district, place?.city, place?.region].filter(
            (p): p is string => typeof p === "string" && p.trim().length > 0,
          );

          const address =
            addressParts.length > 0 ? addressParts.join(", ") : `${position.coords.latitude.toFixed(6)}, ${position.coords.longitude.toFixed(6)}`;

          setLocationLabel(address);
          setLocation({
            coordinates: {
              latitude: position.coords.latitude,
              longitude: position.coords.longitude,
            },
            address,
            timestamp: Date.now(),
          });
        }
      } catch (error) {
        console.error("[RootLayout] Failed to fetch location:", error);
        setLocationError("Unable to fetch location");
      }

      // Initialize network monitor for network change detection
      console.log("📶 Initializing network monitor...");
      const networkMonitor = getNetworkMonitor();
      await networkMonitor.initialize({
        debounceMs: 500,
        enableLogging: __DEV__,
      });

      // Setup SIP store handlers for network reconnection
      setupNetworkMonitor();
    };

    initializeApp();

    // Cleanup network monitor and CallKeep on unmount
    return () => {
      resetNetworkMonitor();
      removeCallKeepListeners();
    };
  }, [setupNetworkMonitor, setLocation]);

  useEffect(() => {
    let lastAppState: AppStateStatus = AppState.currentState;

    const subscription = AppState.addEventListener("change", (nextAppState) => {
      const cameToForeground = (lastAppState === "background" || lastAppState === "inactive") && nextAppState === "active";

      lastAppState = nextAppState;

      if (!cameToForeground) {
        return;
      }

      void handleAppForegroundRecovery();
    });

    return () => {
      subscription.remove();
    };
  }, [handleAppForegroundRecovery]);

  return (
    <ThemeProvider value={colorScheme === "dark" ? DarkTheme : DefaultTheme}>
      <Pressable
        style={[styles.locationBar, { paddingTop: insets.top }]}
        onPress={() => setShowLocationPicker(true)}
        onLayout={(event) => setLocationBarHeight(event.nativeEvent.layout.height)}
      >
        <Image source={require("@/assets/images/drawable-xhdpi/ic_location_statusbar.png")} contentFit="contain" style={styles.locationIcon} />
        <Text numberOfLines={1} style={styles.locationText}>
          {locationText}
        </Text>
      </Pressable>
      {/* Overlay header keeps screen layout stable when entry mode changes */}
      {entryMode && (
        <Animated.View
          pointerEvents="box-none"
          entering={FadeInUp.duration(220)}
          exiting={FadeOutUp.duration(180)}
          style={[styles.headerOverlay, { top: locationBarHeight }]}
        >
          <View style={styles.header}>
            <Button
              variant="ghost"
              size="sm"
              style={styles.backButton}
              textStyle={{ fontSize: 18 }}
              onPress={() => useEntryStore.getState().setEntryMode(null)}
            >
              {"ยกเลิก"}
            </Button>
          </View>
        </Animated.View>
      )}
      <Stack>
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="modal" options={{ presentation: "modal", title: "Modal" }} />
      </Stack>
      <StatusBar style="light" />
      <LocationPickerModal
        visible={showLocationPicker}
        onClose={() => setShowLocationPicker(false)}
        topOffset={insets.top + 30}
        presentation={Platform.OS === "android" ? "inline-overlay" : "native-modal"}
      />

      <View style={styles.footerLogo}>
        <Image source={require("@/assets/images/logo_nbtc.png")} contentFit="contain" style={{ width: 40, height: 40 }} />
        <Image source={require("@/assets/images/aw_nstda_color.png")} contentFit="contain" style={{ width: 120, height: 50 }} />
        <Image source={require("@/assets/images/logo_ufpfoundation.png")} contentFit="contain" style={{ width: 40, height: 40 }} />
      </View>
    </ThemeProvider>
  );
}
