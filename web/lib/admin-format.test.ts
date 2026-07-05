import { describe, expect, it } from "vitest";
import {
  formatBytes,
  formatDurationMs,
  formatRelativeTime,
  middleTruncate,
} from "./admin-format";

describe("formatDurationMs", () => {
  it("renders dash for empty or negative values", () => {
    expect(formatDurationMs(null)).toBe("—");
    expect(formatDurationMs(undefined)).toBe("—");
    expect(formatDurationMs(-1)).toBe("—");
  });

  it("renders sub-second values in ms", () => {
    expect(formatDurationMs(0)).toBe("0ms");
    expect(formatDurationMs(45)).toBe("45ms");
    expect(formatDurationMs(999)).toBe("999ms");
  });

  it("renders seconds below one minute", () => {
    expect(formatDurationMs(1000)).toBe("1s");
    expect(formatDurationMs(19_000)).toBe("19s");
    expect(formatDurationMs(59_400)).toBe("59s");
  });

  it("renders minutes and hours with remainders", () => {
    expect(formatDurationMs(60_000)).toBe("1分钟");
    expect(formatDurationMs(192_000)).toBe("3分12秒");
    expect(formatDurationMs(3_600_000)).toBe("1小时");
    expect(formatDurationMs(7_500_000)).toBe("2小时5分");
  });
});

describe("formatRelativeTime", () => {
  const now = 1_751_700_000_000;

  it("renders dash for empty values", () => {
    expect(formatRelativeTime(null, now)).toBe("—");
    expect(formatRelativeTime(0, now)).toBe("—");
  });

  it("renders 刚刚 under one minute and for future timestamps", () => {
    expect(formatRelativeTime(now - 59_999, now)).toBe("刚刚");
    expect(formatRelativeTime(now + 5_000, now)).toBe("刚刚");
  });

  it("renders minutes and hours boundaries", () => {
    expect(formatRelativeTime(now - 60_000, now)).toBe("1分钟前");
    expect(formatRelativeTime(now - 3_599_999, now)).toBe("59分钟前");
    expect(formatRelativeTime(now - 3_600_000, now)).toBe("1小时前");
    expect(formatRelativeTime(now - 23 * 3_600_000, now)).toBe("23小时前");
  });

  it("renders 昨天 and day counts under 30 days", () => {
    expect(formatRelativeTime(now - 24 * 3_600_000, now)).toBe("昨天");
    expect(formatRelativeTime(now - 3 * 24 * 3_600_000, now)).toBe("3天前");
    expect(formatRelativeTime(now - 29 * 24 * 3_600_000, now)).toBe("29天前");
  });

  it("falls back to a date beyond 30 days", () => {
    const out = formatRelativeTime(now - 31 * 24 * 3_600_000, now);
    expect(out).toMatch(/\d{4}/);
  });
});

describe("formatBytes", () => {
  it("covers unit boundaries", () => {
    expect(formatBytes(null)).toBe("—");
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(18)).toBe("18 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(10_485_760)).toBe("10.0 MB");
    expect(formatBytes(2_147_483_648)).toBe("2.00 GB");
  });
});

describe("middleTruncate", () => {
  it("keeps short strings intact", () => {
    expect(middleTruncate("abc", 10)).toBe("abc");
  });

  it("truncates the middle keeping both ends", () => {
    const long = "asset_history|CN|cn_exchange_fund|sh|510300|none|close";
    const out = middleTruncate(long, 20);
    expect(out.length).toBeLessThanOrEqual(20);
    expect(out.startsWith("asset_his")).toBe(true);
    expect(out.endsWith("e|close")).toBe(true);
    expect(out).toContain("…");
  });
});
