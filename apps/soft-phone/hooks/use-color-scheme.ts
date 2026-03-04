import { useSettingsStore, type ThemePreference } from "@/store/settings-store";
import { useColorScheme as useNativeColorScheme, type ColorSchemeName } from "react-native";

const resolveTheme = (preference: ThemePreference, systemScheme: ColorSchemeName) => {
  if (preference === "system") {
    return systemScheme ?? "light";
  }
  return preference;
};

export function useColorScheme() {
  const nativeColorScheme = useNativeColorScheme();
  const themePreference = useSettingsStore((state) => state.themePreference);

  return resolveTheme(themePreference, nativeColorScheme);
}

export { resolveTheme };
