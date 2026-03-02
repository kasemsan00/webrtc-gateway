import { Platform } from "react-native";

// ─── Base palette ───────────────────────────────────────────────────
export const palette = {
  // Slate (backgrounds & surfaces)
  slate50: "#F8FAFC",
  slate100: "#F1F5F9",
  slate200: "#E2E8F0",
  slate300: "#CBD5E1",
  slate400: "#94A3B8",
  slate500: "#64748B",
  slate600: "#475569",
  slate700: "#334155",
  slate800: "#1E293B",
  slate900: "#0F172A",
  slate950: "#0B1220",

  // Gray (neutral UI)
  gray50: "#F9FAFB",
  gray100: "#F3F4F6",
  gray200: "#E5E7EB",
  gray300: "#D1D5DB",
  gray400: "#9CA3AF",
  gray500: "#6B7280",
  gray600: "#4B5563",
  gray700: "#374151",
  gray800: "#1F2937",
  gray900: "#111827",

  // Indigo (primary / accent)
  indigo400: "#818CF8",
  indigo500: "#6366F1",
  indigoLight: "#A5B4FC",

  // Emerald (success / active)
  emerald500: "#10B981",

  // Red (danger / destructive)
  red400: "#F87171",
  red500: "#EF4444",
  red600: "#DC2626",
  red700: "#B91C1C",

  // Amber (warning / reconnecting)
  amber500: "#F59E0B",

  // Sky (info)
  sky400: "#38BDF8",

  // Blue (links / accents)
  blue500: "#3B82F6",
  blue600: "#2196F3",
  brandBlue: "#0384e2",
  headerBlue: "#26495c",
  linkBlue: "#2687c8",

  // Material-ish tones (ConnectionStatus)
  materialOrange: "#FF9800",
  materialRed: "#F44336",
  materialGrey: "#9E9E9E",

  // Core
  white: "#FFFFFF",
  black: "#000000",
  transparent: "transparent",
} as const;

// ─── Semantic colors ────────────────────────────────────────────────
export const colors = {
  // Backgrounds
  background: palette.slate900,
  surface: palette.slate800,
  surfaceLight: palette.white,
  card: "rgba(30, 41, 59, 0.6)",
  cardBorder: "rgba(51, 65, 85, 0.3)",
  inputBg: "rgba(15, 23, 42, 0.6)",
  inputBorder: "rgba(51, 65, 85, 0.5)",
  overlay: "rgba(0, 0, 0, 0.5)",
  overlayDark: "rgba(0, 0, 0, 0.75)",
  overlayInfo: "rgba(0, 0, 0, 0.3)",

  // Text
  textPrimary: palette.slate100,
  textSecondary: palette.slate400,
  textMuted: palette.slate500,
  textDark: palette.gray900,
  textWhite: palette.white,

  // Borders
  border: "rgba(255, 255, 255, 0.1)",
  borderSubtle: "rgba(51, 65, 85, 0.5)",

  // Status
  success: palette.emerald500,
  danger: palette.red500,
  dangerDark: palette.red600,
  warning: palette.amber500,
  info: palette.blue600,

  // Primary
  primary: palette.indigo500,
  primaryLight: palette.indigoLight,
  primarySubtle: "rgba(99, 102, 241, 0.2)",
  primaryOverlay: "rgba(99, 102, 241, 0.5)",

  // Call-specific
  hangup: palette.red500,
  hangupPressed: palette.red600,
  reconnectingBg: "rgba(245, 158, 11, 0.2)",
  reconnectingBorder: "rgba(245, 158, 11, 0.5)",
  dialpadKey: "rgba(51, 65, 85, 0.8)",
  dialpadKeyPressed: "rgba(71, 85, 105, 0.9)",
  iconBorder: palette.white,
  iconActiveOverlay: "rgba(255, 255, 255, 0.2)",

  // Header / branding
  header: palette.headerBlue,
} as const;

// ─── Light / Dark theme (for react-navigation) ─────────────────────
const tintColorLight = "#0a7ea4";
const tintColorDark = "#fff";

export const Colors = {
  light: {
    text: "#11181C",
    background: "#fff",
    tint: tintColorLight,
    icon: "#687076",
    tabIconDefault: "#687076",
    tabIconSelected: tintColorLight,
  },
  dark: {
    text: "#ECEDEE",
    background: "#151718",
    tint: tintColorDark,
    icon: "#9BA1A6",
    tabIconDefault: "#9BA1A6",
    tabIconSelected: tintColorDark,
  },
} as const;

// ─── Fonts ──────────────────────────────────────────────────────────
export const Fonts = Platform.select({
  ios: {
    sans: "system-ui",
    serif: "ui-serif",
    rounded: "ui-rounded",
    mono: "ui-monospace",
  },
  default: {
    sans: "normal",
    serif: "serif",
    rounded: "normal",
    mono: "monospace",
  },
  web: {
    sans: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif",
    serif: "Georgia, 'Times New Roman', serif",
    rounded: "'SF Pro Rounded', 'Hiragino Maru Gothic ProN', Meiryo, 'MS PGothic', sans-serif",
    mono: "SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
  },
});
