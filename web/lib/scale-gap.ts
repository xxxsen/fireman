/** Matches td/017 §4.1: ±100 minor (1 CNY) scale gap tolerance. */
export const SCALE_GAP_TOLERANCE_MINOR = 100;

export function isSignificantScaleGap(gapMinor: number): boolean {
  return Math.abs(gapMinor) > SCALE_GAP_TOLERANCE_MINOR;
}
