/**
 * VRS Public Extension API
 * Fetches temporary SIP credentials from TTRS VRS API
 */

export interface PublicExtensionResponse {
  status: string;
  data: {
    domain: string;
    domain_caption: string;
    domain_video: string;
    ext: string;
    iss: string;
    name: string;
    secret: string;
    websocket: string;
    iat: number;
    exp: number;
    dtmcreated: string;
    threadid: string;
    uuid: string;
    identification: string;
  };
}

export interface PublicExtensionCredentials {
  sipDomain: string;
  sipUsername: string;
  sipPassword: string;
  sipPort: number;
  exp: number;
  uuid: string;
}

// Cache for public extension credentials
let cachedCredentials: PublicExtensionCredentials | null = null;
let cacheExpiry: number = 0;

const API_URL = "https://vrswebapi.ttrs.in.th/extension/public";
const CACHE_BUFFER_SECONDS = 30; // Refresh 30s before expiry

/**
 * Fetch public extension credentials from VRS API
 * Cached until expiry - 30s
 */
export async function fetchPublicExtension(): Promise<PublicExtensionCredentials> {
  // Return cached credentials if still valid
  const now = Math.floor(Date.now() / 1000);
  if (cachedCredentials && now < cacheExpiry) {
    console.log("[PublicExtension] Using cached credentials (expires in " + (cacheExpiry - now) + "s)");
    return cachedCredentials;
  }

  console.log("[PublicExtension] Fetching new credentials from API...");

  try {
    const response = await fetch(API_URL, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        type: "mobile-public",
        agency: "spinsoft",
        phone: "0828955697",
        fullName: "Example",
        emergency: 0,
        emergency_options_data: "",
        user_agent: "ios",
        mobileUID: "exampleUID",
      }),
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const json: PublicExtensionResponse = await response.json();

    // Validate response
    if (json.status !== "OK") {
      throw new Error(`API returned status: ${json.status}`);
    }

    if (!json.data || !json.data.domain || !json.data.ext || !json.data.secret || !json.data.exp) {
      throw new Error("Missing required fields in API response");
    }

    // Map to SIP credentials
    const credentials: PublicExtensionCredentials = {
      sipDomain: json.data.domain, // Use data.domain as confirmed
      sipUsername: json.data.ext,
      sipPassword: json.data.secret,
      sipPort: 5060, // Default SIP port
      exp: json.data.exp,
      uuid: json.data.uuid,
    };

    // Cache credentials until expiry - buffer
    cacheExpiry = credentials.exp - CACHE_BUFFER_SECONDS;
    cachedCredentials = credentials;

    console.log("[PublicExtension] ✅ Credentials fetched successfully");
    console.log("[PublicExtension] Domain:", credentials.sipDomain);
    console.log("[PublicExtension] Extension:", credentials.sipUsername);
    console.log("[PublicExtension] Expires:", new Date(credentials.exp * 1000).toISOString());

    return credentials;
  } catch (error) {
    console.error("[PublicExtension] ❌ Failed to fetch credentials:", error);
    throw new Error(`Failed to get public extension: ${error instanceof Error ? error.message : String(error)}`);
  }
}

/**
 * Clear cached credentials (useful for logout/reset)
 */
export function clearPublicExtensionCache(): void {
  cachedCredentials = null;
  cacheExpiry = 0;
  console.log("[PublicExtension] Cache cleared");
}

/**
 * Check if cached credentials are still valid
 */
export function hasValidPublicExtensionCache(): boolean {
  const now = Math.floor(Date.now() / 1000);
  return cachedCredentials !== null && now < cacheExpiry;
}
