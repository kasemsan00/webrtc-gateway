/**
 * useNetworkMonitor Hook
 * React hook for network state in components
 */

import { useEffect, useState } from "react";
import {
  getNetworkMonitor,
  NetworkState,
  NetworkChangeEvent,
} from "@/lib/network";

/**
 * Hook to access network state in React components
 * Automatically updates when network state changes
 */
export function useNetworkMonitor() {
  const [networkState, setNetworkState] = useState<NetworkState | null>(null);
  const [lastChange, setLastChange] = useState<NetworkChangeEvent | null>(null);

  useEffect(() => {
    const monitor = getNetworkMonitor();

    // Get current state
    setNetworkState(monitor.getCurrentState());

    // Listen for changes
    const unsubscribe = monitor.addListener((event) => {
      setNetworkState(event.currentState);
      setLastChange(event);
    });

    return unsubscribe;
  }, []);

  return {
    /** Current network state */
    networkState,
    /** Last network change event */
    lastChange,
    /** Whether device is connected to network */
    isConnected: networkState?.isConnected ?? false,
    /** Current IP address */
    ipAddress: networkState?.ipAddress ?? null,
    /** Network type (wifi, cellular, etc.) */
    networkType: networkState?.type ?? "unknown",
    /** WiFi SSID if connected to WiFi */
    ssid: networkState?.ssid ?? null,
  };
}
