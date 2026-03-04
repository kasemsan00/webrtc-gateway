/**
 * CallOverlays - Root-level call modals
 *
 * Renders InCallScreen, IncomingCallModal, and PermissionErrorModal at the root layout level
 * so they display correctly regardless of which tab is active.
 *
 * React Navigation detaches inactive tab screens' native views, which prevents
 * <Modal> components inside those screens from appearing. By rendering here
 * (in app/_layout.tsx), the modals are always in the active native view hierarchy.
 */

import { CallState } from "@/lib/gateway";
import { useDialerStore } from "@/store/dialer-store";
import { useSipStore } from "@/store/sip-store";
import React, { useCallback } from "react";

import { InCallScreen } from "./in-call-screen";
import { IncomingCallModal } from "./incoming-call-modal";
import { PermissionErrorModal } from "./permission-error-modal";

const MAX_PERMISSION_RETRIES = 3;

export function CallOverlays() {
  // SIP store values (individual selectors to avoid unnecessary re-renders)
  const callState = useSipStore((s) => s.callState);
  const remoteNumber = useSipStore((s) => s.remoteNumber);
  const remoteDisplayName = useSipStore((s) => s.remoteDisplayName);
  const callDuration = useSipStore((s) => s.callDuration);
  const incomingCall = useSipStore((s) => s.incomingCall);
  const isMuted = useSipStore((s) => s.isMuted);
  const isSpeaker = useSipStore((s) => s.isSpeaker);
  const isVideoEnabled = useSipStore((s) => s.isVideoEnabled);
  const localStream = useSipStore((s) => s.localStream);
  const remoteStream = useSipStore((s) => s.remoteStream);
  const networkReconnecting = useSipStore((s) => s.networkReconnecting);

  // Permission error state
  const permissionError = useSipStore((s) => s.permissionError);
  const permissionRetryCount = useSipStore((s) => s.permissionRetryCount);
  const missingPermissions = useSipStore((s) => s.missingPermissions);

  // SIP store actions
  const answer = useSipStore((s) => s.answer);
  const decline = useSipStore((s) => s.decline);
  const hangup = useSipStore((s) => s.hangup);
  const toggleMute = useSipStore((s) => s.toggleMute);
  const toggleSpeaker = useSipStore((s) => s.toggleSpeaker);
  const toggleVideo = useSipStore((s) => s.toggleVideo);
  const switchCamera = useSipStore((s) => s.switchCamera);
  const setPermissionError = useSipStore((s) => s.setPermissionError);
  const resetPermissionRetry = useSipStore((s) => s.resetPermissionRetry);

  // Dialer store for fallback phone number display
  const phoneNumber = useDialerStore((s) => s.draftNumber);

  const isInCall =
    callState === CallState.INCALL ||
    callState === CallState.CALLING ||
    callState === CallState.RINGING ||
    callState === CallState.CONNECTING;

  const handleHangup = useCallback(async () => {
    try {
      await hangup();
    } catch (error) {
      console.error("[CallOverlays] Failed to hangup:", error);
    }
  }, [hangup]);

  const handleAnswer = useCallback(async () => {
    try {
      await answer();
    } catch (error) {
      console.error("[CallOverlays] Failed to answer:", error);
    }
  }, [answer]);

  const handleDecline = useCallback(async () => {
    try {
      await decline();
    } catch (error) {
      console.error("[CallOverlays] Failed to decline:", error);
    }
  }, [decline]);

  const handlePermissionRetry = useCallback(async () => {
    setPermissionError(null, []);
  }, [setPermissionError]);

  const handlePermissionCancel = useCallback(() => {
    resetPermissionRetry();
  }, [resetPermissionRetry]);

  return (
    <>
      <InCallScreen
        visible={isInCall}
        phoneNumber={remoteNumber || phoneNumber}
        contactName={remoteDisplayName || undefined}
        callState={callState}
        duration={callDuration}
        isMuted={isMuted}
        isSpeaker={isSpeaker}
        isVideoEnabled={isVideoEnabled}
        localStream={localStream}
        remoteStream={remoteStream}
        networkReconnecting={networkReconnecting}
        onMuteToggle={toggleMute}
        onSpeakerToggle={toggleSpeaker}
        onVideoToggle={toggleVideo}
        onSwitchCamera={switchCamera}
        onHangup={handleHangup}
      />
      <IncomingCallModal
        visible={callState === CallState.INCOMING}
        callInfo={incomingCall}
        onAnswer={handleAnswer}
        onDecline={handleDecline}
      />
      <PermissionErrorModal
        visible={!!permissionError}
        missingPermissions={missingPermissions}
        retryCount={permissionRetryCount}
        maxRetries={MAX_PERMISSION_RETRIES}
        onRetry={handlePermissionRetry}
        onCancel={handlePermissionCancel}
      />
    </>
  );
}
