/** Scale-gap tolerance: ±100 minor units (1 CNY). */
export const SCALE_GAP_TOLERANCE_MINOR = 100;

export function isSignificantScaleGap(gapMinor: number): boolean {
  return Math.abs(gapMinor) > SCALE_GAP_TOLERANCE_MINOR;
}
