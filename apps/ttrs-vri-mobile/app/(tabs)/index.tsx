import { useCallback, useEffect, useState } from "react";
import { ScrollView, View } from "react-native";

import { EntryFormView } from "@/components/softphone/entry-form-view";
import { InCallScreen } from "@/components/softphone/in-call-screen";
import { ModeSelectionView } from "@/components/softphone/mode-selection-view";
import { PermissionErrorModal } from "@/components/softphone/permission-error-modal";
import { useEntrySubmit } from "@/hooks/use-entry-submit";
import { CallState } from "@/lib/gateway";
import { useEntryStore } from "@/store/entry-store";
import { useSipStore } from "@/store/sip-store";
import { styles } from "@/styles/screens/dialer.styles";

const MAX_PERMISSION_RETRIES = 3;

export default function DialerScreen() {
  const [isLoading, setIsLoading] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [lastExtension, setLastExtension] = useState<string | null>(null);
  const entryMode = useEntryStore((s) => s.entryMode);
  const setEntryMode = useEntryStore((s) => s.setEntryMode);
  const [formValues, setFormValues] = useState({ fullName: "", phone: "", department: "" });
  const [formErrors, setFormErrors] = useState<{ fullName?: string; phone?: string; department?: string }>({});

  // Clear form errors when user presses back in header
  useEffect(() => {
    if (!entryMode) setFormErrors({});
  }, [entryMode]);

  // SIP store values
  const callState = useSipStore((s) => s.callState);
  const remoteNumber = useSipStore((s) => s.remoteNumber);
  const remoteDisplayName = useSipStore((s) => s.remoteDisplayName);
  const callDuration = useSipStore((s) => s.callDuration);
  const isMuted = useSipStore((s) => s.isMuted);
  const isSpeaker = useSipStore((s) => s.isSpeaker);
  const isVideoEnabled = useSipStore((s) => s.isVideoEnabled);
  const cameraFacing = useSipStore((s) => s.cameraFacing);
  const localStream = useSipStore((s) => s.localStream);
  const remoteStream = useSipStore((s) => s.remoteStream);
  const networkReconnecting = useSipStore((s) => s.networkReconnecting);
  const isAutoRecovering = useSipStore((s) => s.isAutoRecovering);
  const lastRecoverableError = useSipStore((s) => s.lastRecoverableError);
  const connectionError = useSipStore((s) => s.connectionError);

  // Permission error state
  const permissionError = useSipStore((s) => s.permissionError);
  const permissionRetryCount = useSipStore((s) => s.permissionRetryCount);
  const missingPermissions = useSipStore((s) => s.missingPermissions);

  // SIP store actions
  const hangup = useSipStore((s) => s.hangup);
  const toggleMute = useSipStore((s) => s.toggleMute);
  const toggleSpeaker = useSipStore((s) => s.toggleSpeaker);
  const toggleVideo = useSipStore((s) => s.toggleVideo);
  const switchCamera = useSipStore((s) => s.switchCamera);
  const setPermissionError = useSipStore((s) => s.setPermissionError);
  const resetPermissionRetry = useSipStore((s) => s.resetPermissionRetry);

  const { handleSubmit, validateForm } = useEntrySubmit({
    formValues,
    entryMode,
    onSuccess: (ext) => setLastExtension(ext),
    onError: setErrorMessage,
    onSetLoading: setIsLoading,
  });

  const handleFormSubmit = useCallback(() => {
    const validation = validateForm();
    if (!validation.isValid && validation.errors) {
      setFormErrors(validation.errors);
      return;
    }
    handleSubmit();
  }, [handleSubmit, validateForm]);

  const handleHangup = useCallback(async () => {
    try {
      await hangup();
      setEntryMode(null);
    } catch (error) {
      console.error("Failed to hangup:", error);
    }
  }, [hangup, setEntryMode]);

  // Permission modal handlers
  const handlePermissionRetry = useCallback(async () => {
    console.log("[DialerScreen] Retrying call after permission denial");
    // The retry will happen when user presses call button again
    // Just close the modal by clearing the error
    setPermissionError(null, []);
  }, [setPermissionError]);

  const handlePermissionCancel = useCallback(() => {
    console.log("[DialerScreen] Permission error cancelled");
    resetPermissionRetry();
  }, [resetPermissionRetry]);

  const isInCall =
    callState === CallState.INCALL || callState === CallState.CALLING || callState === CallState.RINGING || callState === CallState.CONNECTING;
  const recoveryStatusMessage = isAutoRecovering ? "กำลังสร้าง session ใหม่..." : null;
  const recoveryErrorMessage = !isAutoRecovering && lastRecoverableError === "PUBLIC_IDENTITY_CHANGED" ? "Session เดิมไม่ตรงกับ user ใหม่ กรุณาโทรใหม่" : null;
  const displayErrorMessage = errorMessage || recoveryErrorMessage || connectionError;

  return (
    <View style={styles.container}>
      <ScrollView style={styles.scrollView} contentContainerStyle={styles.formContainer} keyboardShouldPersistTaps="handled">
        <View style={styles.contentSection}>
          {entryMode ? (
            <EntryFormView
              mode={entryMode}
              values={formValues}
              errors={formErrors}
              isLoading={isLoading}
              errorMessage={displayErrorMessage}
              onChange={(field, value) => setFormValues((prev) => ({ ...prev, [field]: value }))}
              onSubmit={handleFormSubmit}
            />
          ) : (
            <ModeSelectionView onSelectMode={setEntryMode} disabled={isLoading} />
          )}
        </View>
      </ScrollView>

      {/* In Call Screen */}
      <InCallScreen
        visible={isInCall}
        userMobileNumber={formValues.phone.trim()}
        phoneNumber={remoteNumber || lastExtension || "9999"}
        contactName={remoteDisplayName || undefined}
        callState={callState}
        duration={callDuration}
        isMuted={isMuted}
        isSpeaker={isSpeaker}
        isVideoEnabled={isVideoEnabled}
        cameraFacing={cameraFacing}
        localStream={localStream}
        remoteStream={remoteStream}
        networkReconnecting={networkReconnecting}
        autoRecoveryMessage={recoveryStatusMessage}
        onMuteToggle={toggleMute}
        onSpeakerToggle={toggleSpeaker}
        onVideoToggle={toggleVideo}
        onSwitchCamera={switchCamera}
        onHangup={handleHangup}
      />

      {/* Permission Error Modal */}
      <PermissionErrorModal
        visible={!!permissionError}
        missingPermissions={missingPermissions}
        retryCount={permissionRetryCount}
        maxRetries={MAX_PERMISSION_RETRIES}
        onRetry={handlePermissionRetry}
        onCancel={handlePermissionCancel}
      />
    </View>
  );
}
