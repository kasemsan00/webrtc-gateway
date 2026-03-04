/**
 * SIP Module
 *
 * Re-exports from the Gateway module for backward compatibility
 */

// Re-export from gateway module
export { CallState, GatewayClient, getGatewayClient, resetGatewayClient } from "../gateway";
export type { GatewayCallbacks, GatewayConfig, IncomingCallInfo } from "../gateway";

// Legacy alias for backward compatibility
export { GatewayClient as SipClient } from "../gateway";
