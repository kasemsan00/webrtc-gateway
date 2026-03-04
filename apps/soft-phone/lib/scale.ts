import { moderateScale, moderateVerticalScale, scale, verticalScale } from "react-native-size-matters";

/**
 * Baseline-aware wrappers around `react-native-size-matters`.
 *
 * `react-native-size-matters` internally assumes guidelineBaseWidth=350 and guidelineBaseHeight=680.
 * Our design baseline is iPhone-like 390x844, so we pre-scale inputs to align the baseline.
 *
 * Target behavior (for width-based scaling):
 *   desired: out = size * (deviceWidth / 390)
 *   library:  out = input * (deviceWidth / 350)
 *   => input = size * (350 / 390)
 */

export const DESIGN_BASE_WIDTH = 390;
export const DESIGN_BASE_HEIGHT = 844;

const LIB_BASE_WIDTH = 350;
const LIB_BASE_HEIGHT = 680;

const WIDTH_INPUT_RATIO = LIB_BASE_WIDTH / DESIGN_BASE_WIDTH;
const HEIGHT_INPUT_RATIO = LIB_BASE_HEIGHT / DESIGN_BASE_HEIGHT;

function adaptWidth(size: number): number {
  return size * WIDTH_INPUT_RATIO;
}

function adaptHeight(size: number): number {
  return size * HEIGHT_INPUT_RATIO;
}

/** Width-based linear scaling (use for widths/radius/horizontal paddings). */
export function s(size: number): number {
  return scale(adaptWidth(size));
}

/** Height-based linear scaling (use for heights/vertical offsets where it truly depends on height). */
export function vs(size: number): number {
  return verticalScale(adaptHeight(size));
}

/** Moderate scaling (recommended for font sizes and general spacing). */
export function ms(size: number, factor?: number): number {
  return moderateScale(adaptWidth(size), factor);
}

/** Moderate vertical scaling (for vertical spacing where you want to avoid aggressive growth). */
export function mvs(size: number, factor?: number): number {
  return moderateVerticalScale(adaptHeight(size), factor);
}

