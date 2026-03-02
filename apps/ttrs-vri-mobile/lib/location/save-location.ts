import NetInfo from "@react-native-community/netinfo";

const SAVE_LOCATION_URL = "https://vrsapi.ttrs.or.th/lis/savelocation.php";

export interface SaveLocationParams {
  src: "android" | "ios";
  userid: string;
  network: string;
  lat: number;
  long: number;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function buildSaveLocationUrl(params: SaveLocationParams): string {
  const query = new URLSearchParams({
    src: params.src,
    action: "insert",
    userid: params.userid,
    cellid: "1",
    network: params.network,
    lat: String(params.lat),
    long: String(params.long),
  });

  return `${SAVE_LOCATION_URL}?${query.toString()}`;
}

export async function resolveMobileNetworkCode(): Promise<string> {
  try {
    const state = await NetInfo.fetch();
    if (state.type !== "cellular" || !state.details) {
      return "";
    }

    const details: unknown = state.details;
    if (!isRecord(details)) {
      return "";
    }

    const mobileNetworkCode = details.mobileNetworkCode;
    if (typeof mobileNetworkCode === "string") {
      return mobileNetworkCode;
    }

    if (typeof mobileNetworkCode === "number") {
      return String(mobileNetworkCode);
    }

    return "";
  } catch (error) {
    console.warn("[InCallScreen] Failed to resolve mobile network code:", error);
    return "";
  }
}

export async function sendLocationToLis(params: SaveLocationParams): Promise<void> {
  const url = buildSaveLocationUrl(params);

  try {
    const response = await fetch(url, { method: "GET" });
    if (!response.ok) {
      console.warn("[InCallScreen] Save location API returned non-OK status:", response.status);
    }
  } catch (error) {
    console.error("[InCallScreen] Save location API failed:", error);
  }
}
