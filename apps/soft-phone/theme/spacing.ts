/**
 * Spacing & Sizing Tokens
 *
 * Common spacing, padding, and sizing values used across the app.
 * Using a consistent spacing scale improves visual rhythm.
 */

export const Spacing = {
  /** 4px */
  xs: 4,
  /** 8px */
  sm: 8,
  /** 12px */
  md: 12,
  /** 16px */
  lg: 16,
  /** 20px */
  xl: 20,
  /** 24px */
  xxl: 24,
  /** 32px */
  xxxl: 32,
} as const;

export const BorderRadius = {
  /** 8px — small elements */
  sm: 8,
  /** 10px — buttons, inputs  */
  md: 10,
  /** 12px — cards, grid items */
  lg: 12,
  /** 14px — segmented controls */
  xl: 14,
  /** 18px — large cards */
  xxl: 18,
  /** Full circle */
  full: 999,
} as const;

export const IconSize = {
  sm: 14,
  md: 18,
  lg: 20,
  xl: 24,
  xxl: 48,
} as const;
