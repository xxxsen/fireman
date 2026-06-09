/** User types 3 meaning 3%; API stores 0.03. */

export function percentToDecimal(input: string): number | null {
  const trimmed = input.trim().replace(/%$/, "");
  if (trimmed === "") return null;
  const n = Number(trimmed);
  if (!Number.isFinite(n)) return null;
  return n / 100;
}

export function decimalToPercentString(decimal: number, digits = 2): string {
  const pct = decimal * 100;
  const rounded = Math.round(pct * 10 ** digits) / 10 ** digits;
  return String(rounded);
}

export function formatPercent(decimal: number, digits = 2): string {
  return `${decimalToPercentString(decimal, digits)}%`;
}

export function validatePercentSum(
  items: { label?: string; value: number }[],
  expected = 1,
  tolerance = 0.0001,
): { passed: boolean; total: number; gap: number; message: string } {
  const total = items.reduce((s, i) => s + i.value, 0);
  const gap = expected - total;
  const passed = Math.abs(gap) <= tolerance;
  const gapPct = Math.abs(gap * 100).toFixed(2);
  const message = passed
    ? "合计 100.00%，通过"
    : gap > 0
      ? `合计 ${(total * 100).toFixed(2)}%，还差 ${gapPct}%`
      : `合计 ${(total * 100).toFixed(2)}%，超出 ${gapPct}%`;
  return { passed, total, gap, message };
}
