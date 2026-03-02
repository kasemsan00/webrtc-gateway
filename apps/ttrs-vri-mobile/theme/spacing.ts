export { ms, mvs, s, vs } from "@/lib/scale";

// ─── Base spacing scale ─────────────────────────────────────────────
export const spacing = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  "2xl": 24,
  "3xl": 32,
  "4xl": 40,
} as const;

// ─── Border radii ───────────────────────────────────────────────────
export const radii = {
  sm: 8,
  md: 10,
  lg: 12,
  xl: 16,
  "2xl": 20,
  full: 9999,
} as const;
