import { useSettingsStore, type ThemePreference } from "@/store/settings-store";
import { useEffect, useState } from "react";
import { useColorScheme as useRNColorScheme, type ColorSchemeName } from "react-native";

const resolveTheme = (preference: ThemePreference, systemScheme: ColorSchemeName) => {
  if (preference === "system") {
    return systemScheme ?? "light";
  }
  return preference;
};

/**
 * To support static rendering, this value needs to be re-calculated on the client side for web
 */
export function useColorScheme() {
  const [hasHydrated, setHasHydrated] = useState(false);
  const themePreference = useSettingsStore((state) => state.themePreference);

  useEffect(() => {
    setHasHydrated(true);
  }, []);

  const colorScheme = useRNColorScheme();

  if (hasHydrated) {
    return resolveTheme(themePreference, colorScheme);
  }

  return "light";
}
