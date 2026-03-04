/**
 * Connection Status Component
 *
 * Displays connection status and reconnection progress
 */

import { useSettingsStore } from "@/store/settings-store";
import { useSipStore } from "@/store/sip-store";
import { styles } from "@/styles/components/ConnectionStatus.styles";
import { ActivityIndicator, Pressable, Text, View } from "react-native";

export function ConnectionStatus() {
  const callMode = useSettingsStore((s) => s.callMode);
  const { isConnected, isConnecting, isReconnecting, reconnectAttempt, maxReconnectAttempts, trunkResolveStatus, reconnectFailed, manualReconnect } =
    useSipStore();

  const isReady = callMode === "public" ? isConnected : isConnected && trunkResolveStatus === "resolved";

  // Don't show if everything is normal
  if (isReady && !isReconnecting && !reconnectFailed) {
    return null;
  }

  return (
    <View style={styles.container}>
      {isConnecting && (
        <View style={styles.statusRow}>
          <ActivityIndicator size="small" color="#fff" />
          <Text style={styles.statusText}>Connecting...</Text>
        </View>
      )}

      {isReconnecting && (
        <View style={[styles.statusRow, styles.reconnecting]}>
          <ActivityIndicator size="small" color="#fff" />
          <Text style={styles.statusText}>
            Reconnecting... ({reconnectAttempt}/{maxReconnectAttempts})
          </Text>
        </View>
      )}

      {reconnectFailed && (
        <Pressable style={({ pressed }) => [styles.statusRow, styles.failed, pressed && styles.pressedRow]} onPress={manualReconnect}>
          <Text style={styles.statusText}>⚠️ Connection lost. Tap to retry</Text>
        </Pressable>
      )}

      {!isConnected && !isConnecting && !isReconnecting && !reconnectFailed && (
        <Pressable style={({ pressed }) => [styles.statusRow, styles.disconnected, pressed && styles.pressedRow]} onPress={manualReconnect}>
          <Text style={styles.statusText}>⚠️ Disconnected. Tap to reconnect</Text>
        </Pressable>
      )}

      {isConnected && callMode === "siptrunk" && trunkResolveStatus !== "resolved" && !isReconnecting && (
        <View style={[styles.statusRow, styles.warning]}>
          <ActivityIndicator size="small" color="#fff" />
          <Text style={styles.statusText}>Resolving trunk...</Text>
        </View>
      )}
    </View>
  );
}
