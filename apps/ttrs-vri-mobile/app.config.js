/**
 * Expo config - อ่านจาก app.json และ inject ค่าจาก env (เช่น Google Maps API key).
 * ใช้กับ prebuild / development build เท่านั้น (Expo โหลด .env ให้เมื่อมี expo-dotenv หรือ built-in support)
 */
module.exports = ({ config }) => {
  const googleMapsApiKey = process.env.GOOGLE_MAPS_API_KEY ?? "";
  const androidConfig = config?.android ?? {};

  return {
    ...config,
    android: {
      ...androidConfig,
      config: {
        ...(androidConfig.config ?? {}),
        googleMaps: {
          apiKey: googleMapsApiKey,
        },
      },
    },
  };
};
