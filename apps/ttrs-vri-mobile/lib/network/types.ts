/**
 * Network Types
 * Types for network monitoring and IP change detection
 */

import type { NetInfoStateType } from "@react-native-community/netinfo";

/**
 * Represents the current network state
 */
export interface NetworkState {
  isConnected: boolean;
  isInternetReachable: boolean | null;
  type: NetInfoStateType;
  ipAddress: string | null;
  previousIpAddress: string | null;
  ssid: string | null;
}

/**
 * Network change event types
 */
export type NetworkChangeType =
  | "ip_changed"
  | "connection_lost"
  | "connection_restored"
  | "network_type_changed";

/**
 * Network change event emitted when network state changes
 */
export interface NetworkChangeEvent {
  type: NetworkChangeType;
  previousState: NetworkState;
  currentState: NetworkState;
  timestamp: number;
}

/**
 * Configuration for the network monitor
 */
export interface NetworkMonitorConfig {
  /** Debounce rapid network events in ms (default: 500) */
  debounceMs: number;
  /** Delay before triggering reconnect after IP change in ms (default: 100) */
  ipChangeReconnectDelay: number;
  /** Enable debug logging (default: false) */
  enableLogging: boolean;
}

/**
 * Default configuration values
 */
export const DEFAULT_NETWORK_MONITOR_CONFIG: NetworkMonitorConfig = {
  debounceMs: 500,
  ipChangeReconnectDelay: 100,
  enableLogging: __DEV__ ?? false,
};

/**
 * Saved call state for resumption after network change
 */
export interface SavedCallState {
  sessionId: string | null;
  remoteNumber: string | null;
  wasInCall: boolean;
  timestamp: number;
}
