/**
 * Network Module
 * Exports network monitoring utilities
 */

export {
  getNetworkMonitor,
  resetNetworkMonitor,
  NetworkMonitorService,
} from "./network-monitor";

export type {
  NetworkState,
  NetworkChangeEvent,
  NetworkChangeType,
  NetworkMonitorConfig,
  SavedCallState,
} from "./types";

export { DEFAULT_NETWORK_MONITOR_CONFIG } from "./types";
