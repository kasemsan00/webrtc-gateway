/**
 * Gateway Module
 * Export gateway client and types
 */

export { GatewayClient, getGatewayClient, resetGatewayClient, type CallAuth, type PublicCallAuth, type TrunkCallAuth } from "./gateway-client";
export { CallState, DEFAULT_RECONNECT_CONFIG, GatewayState } from "./types";
export type {
  ConnectionState,
  DtmfMessage,
  GatewayCallbacks,
  GatewayConfig,
  RecoverableGatewayErrorCode,
  RecoveryState,
  GatewayRttMessage,
  IncomingMessage,
  OutgoingMessage,
  ReconnectConfig,
} from "./types";
