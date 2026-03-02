/**
 * Network Monitor Service
 * Monitors network/IP changes and triggers proactive WebSocket reconnection
 */

import NetInfo, { NetInfoState, addEventListener } from "@react-native-community/netinfo";
import { DEFAULT_NETWORK_MONITOR_CONFIG, NetworkChangeEvent, NetworkMonitorConfig, NetworkState, SavedCallState } from "./types";

type NetworkChangeCallback = (event: NetworkChangeEvent) => void;

/**
 * NetworkMonitorService
 * Singleton service that monitors network state and triggers reconnection
 * when IP address changes (e.g., WiFi to 5G switch)
 */
export class NetworkMonitorService {
  private unsubscribe: (() => void) | null = null;
  private currentState: NetworkState | null = null;
  private config: NetworkMonitorConfig = DEFAULT_NETWORK_MONITOR_CONFIG;
  private callbacks: Set<NetworkChangeCallback> = new Set();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;
  private isInitialized = false;

  // Call state preservation for resumption
  private savedCallState: SavedCallState | null = null;

  // Reconnection handler (set externally by sip-store)
  private reconnectHandler: (() => Promise<void>) | null = null;
  private resumeCallHandler: ((savedState: SavedCallState) => Promise<void>) | null = null;

  /**
   * Initialize the network monitor
   */
  async initialize(config?: Partial<NetworkMonitorConfig>): Promise<void> {
    if (this.isInitialized) {
      this.log("Already initialized");
      return;
    }

    this.config = { ...DEFAULT_NETWORK_MONITOR_CONFIG, ...config };

    // Get initial network state
    const initialState = await NetInfo.fetch();
    this.currentState = this.mapNetInfoState(initialState, null);
    this.log("Initial network state:", this.currentState);

    // Subscribe to network changes
    this.unsubscribe = addEventListener(this.handleNetworkChange.bind(this));
    this.isInitialized = true;
    this.log("Network monitor initialized");
  }

  /**
   * Stop monitoring network changes
   */
  stop(): void {
    if (this.unsubscribe) {
      this.unsubscribe();
      this.unsubscribe = null;
    }
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    this.isInitialized = false;
    this.log("Network monitor stopped");
  }

  /**
   * Set the reconnection handler (called from sip-store)
   */
  setReconnectHandler(handler: () => Promise<void>): void {
    this.reconnectHandler = handler;
  }

  /**
   * Set the call resume handler (called from sip-store)
   */
  setResumeCallHandler(handler: (savedState: SavedCallState) => Promise<void>): void {
    this.resumeCallHandler = handler;
  }

  /**
   * Save call state for potential resumption
   * Call this before network-triggered disconnection
   */
  saveCallState(sessionId: string | null, remoteNumber: string | null, wasInCall: boolean): void {
    if (wasInCall && (sessionId || remoteNumber)) {
      this.savedCallState = {
        sessionId,
        remoteNumber,
        wasInCall,
        timestamp: Date.now(),
      };
      this.log("Call state saved:", this.savedCallState);
    } else {
      this.savedCallState = null;
    }
  }

  /**
   * Get saved call state
   */
  getSavedCallState(): SavedCallState | null {
    return this.savedCallState;
  }

  /**
   * Clear saved call state
   */
  clearSavedCallState(): void {
    this.savedCallState = null;
  }

  /**
   * Add callback for network change events
   */
  addListener(callback: NetworkChangeCallback): () => void {
    this.callbacks.add(callback);
    return () => this.callbacks.delete(callback);
  }

  /**
   * Get current network state
   */
  getCurrentState(): NetworkState | null {
    return this.currentState;
  }

  /**
   * Check if currently connected
   */
  isConnected(): boolean {
    return this.currentState?.isConnected ?? false;
  }

  /**
   * Get current IP address
   */
  getIpAddress(): string | null {
    return this.currentState?.ipAddress ?? null;
  }

  /**
   * Handle network state change from NetInfo
   */
  private handleNetworkChange(state: NetInfoState): void {
    // Debounce rapid network events
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
    }

    this.debounceTimer = setTimeout(() => {
      this.processNetworkChange(state);
    }, this.config.debounceMs);
  }

  /**
   * Process debounced network change
   */
  private async processNetworkChange(state: NetInfoState): Promise<void> {
    const previousState = this.currentState;
    const newState = this.mapNetInfoState(state, previousState?.ipAddress ?? null);

    this.currentState = newState;

    // Determine change type
    const event = this.determineChangeEvent(previousState, newState);

    if (event) {
      this.log("Network change detected:", event.type, {
        previousIp: previousState?.ipAddress,
        newIp: newState.ipAddress,
        previousType: previousState?.type,
        newType: newState.type,
      });

      // Notify listeners
      this.notifyCallbacks(event);

      // Handle reconnection
      await this.handleReconnectionStrategy(event);
    }
  }

  /**
   * Map NetInfo state to our NetworkState type
   */
  private mapNetInfoState(state: NetInfoState, previousIp: string | null): NetworkState {
    let ipAddress: string | null = null;
    let ssid: string | null = null;

    // Extract IP address based on connection type
    if (state.type === "wifi" && state.details) {
      ipAddress = (state.details as any).ipAddress || null;
      ssid = (state.details as any).ssid || null;
    } else if (state.type === "cellular" && state.details) {
      ipAddress = (state.details as any).ipAddress || null;
    }

    return {
      isConnected: state.isConnected ?? false,
      isInternetReachable: state.isInternetReachable,
      type: state.type,
      ipAddress,
      previousIpAddress: previousIp,
      ssid,
    };
  }

  /**
   * Determine the type of network change event
   */
  private determineChangeEvent(prev: NetworkState | null, curr: NetworkState): NetworkChangeEvent | null {
    if (!prev) return null;

    const timestamp = Date.now();
    const baseEvent = { previousState: prev, currentState: curr, timestamp };

    // Connection lost
    if (prev.isConnected && !curr.isConnected) {
      return { type: "connection_lost", ...baseEvent };
    }

    // Connection restored
    if (!prev.isConnected && curr.isConnected) {
      return { type: "connection_restored", ...baseEvent };
    }

    // IP address changed (most critical for VoIP)
    if (prev.ipAddress && curr.ipAddress && prev.ipAddress !== curr.ipAddress) {
      return { type: "ip_changed", ...baseEvent };
    }

    // Network type changed (e.g., WiFi -> Cellular) without IP change
    // This can happen if the IP change detection is delayed
    if (prev.type !== curr.type && curr.isConnected) {
      return { type: "network_type_changed", ...baseEvent };
    }

    return null;
  }

  /**
   * Handle reconnection based on change type
   */
  private async handleReconnectionStrategy(event: NetworkChangeEvent): Promise<void> {
    switch (event.type) {
      case "ip_changed":
      case "network_type_changed":
        this.log("IP/Network changed - initiating proactive reconnect");

        // Small delay to let new network stabilize
        await this.delay(this.config.ipChangeReconnectDelay);

        // Trigger proactive reconnection
        await this.performProactiveReconnect();
        break;

      case "connection_lost":
        this.log("Connection lost - waiting for network to restore");
        // Don't do anything - wait for connection_restored
        break;

      case "connection_restored":
        this.log("Connection restored - attempting reconnect");
        await this.delay(500); // Wait for network to stabilize
        await this.performProactiveReconnect();
        break;
    }
  }

  /**
   * Perform proactive reconnection
   */
  private async performProactiveReconnect(): Promise<void> {
    if (!this.reconnectHandler) {
      this.log("No reconnect handler set - skipping proactive reconnect");
      return;
    }

    try {
      this.log("Starting proactive reconnect...");
      await this.reconnectHandler();
      this.log("Proactive reconnect successful");

      // Attempt call resumption if we were in a call
      if (this.savedCallState?.wasInCall && this.resumeCallHandler) {
        await this.attemptCallResumption();
      }
    } catch (error) {
      this.log("Proactive reconnect failed:", error);
      // Let normal reconnection logic handle retries
    }
  }

  /**
   * Attempt to resume a call after reconnection
   */
  private async attemptCallResumption(): Promise<void> {
    if (!this.savedCallState || !this.resumeCallHandler) {
      return;
    }

    // Check if saved state is too old (more than 30 seconds)
    const ageMs = Date.now() - this.savedCallState.timestamp;
    if (ageMs > 30000) {
      this.log("Saved call state too old, skipping resumption");
      this.clearSavedCallState();
      return;
    }

    this.log("Attempting call resumption...", this.savedCallState);

    try {
      await this.resumeCallHandler(this.savedCallState);
      this.log("Call resumption initiated");
    } catch (error) {
      this.log("Call resumption failed:", error);
    } finally {
      this.clearSavedCallState();
    }
  }

  /**
   * Notify all registered callbacks
   */
  private notifyCallbacks(event: NetworkChangeEvent): void {
    this.callbacks.forEach((cb) => {
      try {
        cb(event);
      } catch (error) {
        console.error("[NetworkMonitor] Callback error:", error);
      }
    });
  }

  /**
   * Helper: delay execution
   */
  private delay(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  /**
   * Helper: conditional logging
   */
  private log(...args: any[]): void {
    if (this.config.enableLogging) {
      console.log("[NetworkMonitor]", ...args);
    }
  }
}

// Singleton instance
let networkMonitorInstance: NetworkMonitorService | null = null;

/**
 * Get the network monitor singleton instance
 */
export function getNetworkMonitor(): NetworkMonitorService {
  if (!networkMonitorInstance) {
    networkMonitorInstance = new NetworkMonitorService();
  }
  return networkMonitorInstance;
}

/**
 * Reset the network monitor (for testing or cleanup)
 */
export function resetNetworkMonitor(): void {
  if (networkMonitorInstance) {
    networkMonitorInstance.stop();
    networkMonitorInstance = null;
  }
}
