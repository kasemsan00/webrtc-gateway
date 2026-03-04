/**
 * Typography Definitions
 *
 * Fonts and common text style presets used across the app.
 */

import { Platform } from "react-native";

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

export const FontSize = {
  /** 11px — badges, summary */
  xs: 11,
  /** 12px — captions, timestamps */
  sm: 12,
  /** 13px — labels, descriptions */
  md: 13,
  /** 14px — secondary text */
  base: 14,
  /** 15px — status values */
  lg: 15,
  /** 16px — body text, default */
  body: 16,
  /** 18px — empty titles */
  xl: 18,
  /** 20px — subtitles */
  xxl: 20,
  /** 24px — dialpad text */
  xxxl: 24,
  /** 28px — header titles */
  heading: 28,
  /** 30px — large header  */
  headingLg: 30,
  /** 32px — title */
  title: 32,
  /** 36px — phone number display */
  display: 36,
} as const;

export const FontWeight = {
  light: "300" as const,
  normal: "400" as const,
  medium: "500" as const,
  semibold: "600" as const,
  bold: "700" as const,
  extrabold: "800" as const,
};
