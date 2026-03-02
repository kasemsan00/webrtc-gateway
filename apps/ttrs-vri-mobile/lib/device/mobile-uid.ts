import * as Keychain from "react-native-keychain";

const MOBILE_UID_KEYCHAIN_SERVICE = "ttrs-vri-mobile-uid";
const MOBILE_UID_KEYCHAIN_USERNAME = "mobile_uid";

function isRecoverableKeychainError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return (
    message.includes("CryptoFailedException") ||
    message.includes("Authentication tag verification failed") ||
    message.includes("decryption failed")
  );
}

function generateUuidV4(): string {
  const cryptoApi = globalThis.crypto;
  if (cryptoApi && typeof cryptoApi.randomUUID === "function") {
    return cryptoApi.randomUUID();
  }

  const randomBytes = new Uint8Array(16);
  if (cryptoApi && typeof cryptoApi.getRandomValues === "function") {
    cryptoApi.getRandomValues(randomBytes);
  } else {
    for (let index = 0; index < randomBytes.length; index += 1) {
      randomBytes[index] = Math.floor(Math.random() * 256);
    }
  }

  randomBytes[6] = (randomBytes[6] & 0x0f) | 0x40;
  randomBytes[8] = (randomBytes[8] & 0x3f) | 0x80;

  const hex = Array.from(randomBytes, (value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export async function getOrCreateMobileUid(): Promise<string> {
  try {
    let existingCredential: false | Keychain.UserCredentials = false;
    try {
      existingCredential = await Keychain.getGenericPassword({
        service: MOBILE_UID_KEYCHAIN_SERVICE,
      });
    } catch (error) {
      if (!isRecoverableKeychainError(error)) {
        throw error;
      }

      console.warn("[mobile-uid] Corrupted keychain entry detected, resetting service data");
      await Keychain.resetGenericPassword({
        service: MOBILE_UID_KEYCHAIN_SERVICE,
      });
    }

    if (existingCredential && existingCredential.password) {
      return existingCredential.password;
    }

    const nextMobileUid = generateUuidV4();
    await Keychain.setGenericPassword(MOBILE_UID_KEYCHAIN_USERNAME, nextMobileUid, {
      service: MOBILE_UID_KEYCHAIN_SERVICE,
    });
    return nextMobileUid;
  } catch (error) {
    console.error("[mobile-uid] Failed to read/write keychain mobile UID:", error);
    return generateUuidV4();
  }
}
