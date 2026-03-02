import NetInfo from "@react-native-community/netinfo";

import { buildSaveLocationUrl, resolveMobileNetworkCode, sendLocationToLis } from "@/lib/location/save-location";

jest.mock("@react-native-community/netinfo", () => ({
  __esModule: true,
  default: {
    fetch: jest.fn(),
  },
}));

describe("save-location helpers", () => {
  const originalFetch = global.fetch;
  const mockedNetInfoFetch = jest.mocked(NetInfo.fetch);

  beforeEach(() => {
    jest.clearAllMocks();
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
    });
  });

  afterAll(() => {
    global.fetch = originalFetch;
  });

  it("builds URL with required GET query params for android", () => {
    const url = buildSaveLocationUrl({
      src: "android",
      userid: "0891234567",
      network: "11",
      lat: 13.7563,
      long: 100.5018,
    });

    const parsed = new URL(url);
    expect(parsed.origin).toBe("https://vrsapi.ttrs.or.th");
    expect(parsed.pathname).toBe("/lis/savelocation.php");
    expect(parsed.searchParams.get("src")).toBe("android");
    expect(parsed.searchParams.get("action")).toBe("insert");
    expect(parsed.searchParams.get("userid")).toBe("0891234567");
    expect(parsed.searchParams.get("cellid")).toBe("1");
    expect(parsed.searchParams.get("network")).toBe("11");
    expect(parsed.searchParams.get("lat")).toBe("13.7563");
    expect(parsed.searchParams.get("long")).toBe("100.5018");
  });

  it("builds URL with iOS src and URL-encodes userid", () => {
    const url = buildSaveLocationUrl({
      src: "ios",
      userid: "089 123 4567",
      network: "",
      lat: 1,
      long: 2,
    });

    const parsed = new URL(url);
    expect(parsed.searchParams.get("src")).toBe("ios");
    expect(parsed.searchParams.get("userid")).toBe("089 123 4567");
  });

  it("sends GET request to LIS endpoint", async () => {
    await sendLocationToLis({
      src: "android",
      userid: "0891234567",
      network: "99",
      lat: 13.1,
      long: 100.2,
    });

    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining("https://vrsapi.ttrs.or.th/lis/savelocation.php?"), { method: "GET" });
  });

  it("returns mobileNetworkCode on cellular network", async () => {
    mockedNetInfoFetch.mockResolvedValue({
      type: "cellular",
      details: {
        mobileNetworkCode: "11",
      },
    });

    await expect(resolveMobileNetworkCode()).resolves.toBe("11");
  });

  it("returns empty string when cellular mnc is unavailable", async () => {
    mockedNetInfoFetch.mockResolvedValue({
      type: "cellular",
      details: {},
    });

    await expect(resolveMobileNetworkCode()).resolves.toBe("");
  });

  it("returns empty string on non-cellular network", async () => {
    mockedNetInfoFetch.mockResolvedValue({
      type: "wifi",
      details: {},
    });

    await expect(resolveMobileNetworkCode()).resolves.toBe("");
  });
});
