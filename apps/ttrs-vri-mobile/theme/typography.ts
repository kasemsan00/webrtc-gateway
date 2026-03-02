// ─── Font sizes ─────────────────────────────────────────────────────
export const fontSize = {
  xs: 10,
  sm: 11,
  md: 12,
  base: 13,
  lg: 14,
  xl: 15,
  "2xl": 16,
  "3xl": 18,
  "4xl": 22,
  "5xl": 24,
  "6xl": 32,
} as const;

// ─── Font weights ───────────────────────────────────────────────────
export const fontWeight = {
  light: "300" as const,
  normal: "400" as const,
  medium: "500" as const,
  semibold: "600" as const,
  bold: "700" as const,
};

// ─── Line heights ───────────────────────────────────────────────────
export const lineHeight = {
  tight: 20,
  normal: 22,
} as const;
