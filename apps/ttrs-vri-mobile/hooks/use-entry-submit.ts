import { getOrCreateMobileUid } from "@/lib/device/mobile-uid";
import type { PublicCallAuth } from "@/lib/gateway";
import { useSipStore } from "@/store/sip-store";
import { useCallback } from "react";
import { Platform } from "react-native";

const API_URL = process.env.EXPO_PUBLIC_API_URL;

type ExtensionPublicResponse =
  | {
      status: "OK";
      data: {
        domain: string;
        ext: string;
        secret: string;
        name?: string;
      };
    }
  | { status: string; data?: unknown };

export interface UseEntrySubmitParams {
  formValues: { fullName: string; phone: string; department: string };
  entryMode: "normal" | "emergency" | null;
  onSuccess?: (ext: string) => void;
  onError?: (message: string | null) => void;
  onSetLoading: (loading: boolean) => void;
}

// Helper to strip port from domain (matches gateway-client implementation)
function stripPortFromDomain(domain: string): string {
  if (!domain) {
    return domain;
  }

  // [IPv6]:port -> IPv6
  if (domain.startsWith("[") && domain.includes("]")) {
    const closingIndex = domain.indexOf("]");
    const hostPart = domain.substring(1, closingIndex);
    return hostPart;
  }

  // hostname:port or IPv4:port -> hostname / IPv4
  if (/:[0-9]+$/.test(domain)) {
    return domain.replace(/:[0-9]+$/, "");
  }

  return domain;
}

export function useEntrySubmit({ formValues, entryMode, onSuccess, onError, onSetLoading }: UseEntrySubmitParams) {
  const connect = useSipStore((s) => s.connect);
  const disconnect = useSipStore((s) => s.disconnect);
  const call = useSipStore((s) => s.call);
  const resetPermissionRetry = useSipStore((s) => s.resetPermissionRetry);
  const isConnected = useSipStore((s) => s.isConnected);

  const isExtensionResponseOk = useCallback(
    (payload: ExtensionPublicResponse): payload is Extract<ExtensionPublicResponse, { status: "OK" }> => payload.status === "OK",
    [],
  );

  const validateForm = useCallback(() => {
    const nextErrors: { fullName?: string; phone?: string; department?: string } = {};

    if (!formValues.fullName.trim()) {
      nextErrors.fullName = "กรุณากรอกชื่อ-นามสกุล";
    }

    if (!/^[0-9]{9,10}$/.test(formValues.phone.trim())) {
      nextErrors.phone = "กรุณากรอกหมายเลขที่ถูกต้อง";
    }

    if (!formValues.department.trim()) {
      nextErrors.department = "กรุณากรอกหน่วยงาน";
    }

    return Object.keys(nextErrors).length === 0 ? { isValid: true, errors: null } : { isValid: false, errors: nextErrors };
  }, [formValues]);

  const handleSubmit = useCallback(async () => {
    if (!entryMode) {
      onError?.("กรุณาเลือกโหมดการใช้งาน");
      return;
    }

    const validation = validateForm();
    if (!validation.isValid) {
      onError?.("กรุณากรอกข้อมูลให้ครบถ้วน");
      return;
    }

    try {
      onSetLoading(true);
      onError?.(null);
      const mobileUID = await getOrCreateMobileUid();
      const requestPayload = {
        type: "mobile-public",
        agency: formValues.department.trim(),
        phone: formValues.phone.trim(),
        fullName: formValues.fullName.trim(),
        emergency: entryMode === "emergency" ? "1" : "0",
        emergency_options_data: "",
        user_agent: Platform.OS,
        mobileUID,
      };

      const response = await fetch(`${API_URL}/extension/public`, {
        method: "POST",
        headers: { Accept: "application/json", "Content-Type": "application/json" },
        body: JSON.stringify(requestPayload),
      });

      if (!response.ok) {
        throw new Error(`API request failed: ${response.status} ${response.statusText}`);
      }

      const payload: ExtensionPublicResponse = await response.json();

      if (!isExtensionResponseOk(payload)) {
        throw new Error(`API returned status: ${payload.status}`);
      }

      const { domain, ext, secret } = payload.data;

      if (!domain || !ext || !secret) {
        throw new Error("API response missing required SIP fields");
      }

      // Build callAuth for per-call authentication (public mode)
      const sipPort = 5060; // Default SIP port
      const hostOnlyDomain = stripPortFromDomain(domain);
      const callAuth: PublicCallAuth = {
        mode: "public",
        sipDomain: hostOnlyDomain,
        sipUsername: ext,
        sipPassword: secret,
        sipPort,
        from: ext, // Use extension as from
      };

      resetPermissionRetry();
      await connect();

      if (entryMode === "emergency") {
        await call("9999", callAuth);
      } else {
        await call("14131", callAuth);
      }

      onSuccess?.(ext);
      console.log("[useEntrySubmit] Form call started for ext:", ext);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error("[useEntrySubmit] Form submit failed:", message);
      onError?.(message);

      // Cleanup: disconnect WebSocket if we connected but call failed
      if (isConnected) {
        console.log("[useEntrySubmit] Disconnecting WebSocket after call failure...");
        await disconnect().catch((disconnectError) => {
          console.error("[useEntrySubmit] Disconnect after call failure failed:", disconnectError);
        });
      }
    } finally {
      onSetLoading(false);
    }
  }, [
    disconnect,
    isConnected,
    call,
    connect,
    entryMode,
    formValues,
    isExtensionResponseOk,
    onError,
    onSuccess,
    onSetLoading,
    resetPermissionRetry,
    validateForm,
  ]);

  return { handleSubmit, validateForm };
}
