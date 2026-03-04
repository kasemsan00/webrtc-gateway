/**
 * Gateway Module
 * Export gateway client and types
 */

export {
  GatewayClient,
  getGatewayClient,
  resetGatewayClient,
  type CallAuth,
  type PublicCallAuth,
  type TrunkCallAuth,
} from './gateway-client';
export { CallState, DEFAULT_RECONNECT_CONFIG, GatewayState } from './types';
export type {
  ConnectionState,
  DtmfMessage,
  GatewayCallbacks,
  GatewayConfig,
  GatewayRttMessage,
  IncomingCallInfo,
  IncomingMessage,
  OutgoingMessage,
  RecoverableGatewayErrorCode,
  ReconnectConfig,
  RecoveryState,
  RegisterStatusResponse,
  TrunkNotFoundResponse,
  TrunkNotReadyResponse,
  TrunkRedirectResponse,
  TrunkResolveMessage,
  TrunkResolvedResponse,
} from './types';
