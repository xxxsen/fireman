// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import nextConfig from "../../next.config";
import { INSTRUMENT_REFRESH_TIMEOUT_MS, MARKET_OPERATION_TIMEOUT_MS } from "./client";

describe("timeout layer hierarchy", () => {
  it("resolve: Next proxy > Web operation > Go > sidecar", () => {
    const nextProxyMs = nextConfig.experimental?.proxyTimeout ?? 0;
    const webOperationMs = MARKET_OPERATION_TIMEOUT_MS;
    const goTimeoutMs = 90_000;
    const sidecarDeadlineMs = 70_000;

    expect(sidecarDeadlineMs).toBeLessThan(goTimeoutMs);
    expect(goTimeoutMs).toBeLessThan(webOperationMs);
    expect(webOperationMs).toBeLessThan(nextProxyMs);
    expect(webOperationMs).toBe(105_000);
  });

  it("refresh: Next proxy > Web refresh > Go fetch > sidecar fetch", () => {
    const nextProxyMs = nextConfig.experimental?.proxyTimeout ?? 0;
    const webRefreshMs = INSTRUMENT_REFRESH_TIMEOUT_MS;
    const goFetchMs = 300_000;
    const sidecarFetchMs = 240_000;

    expect(sidecarFetchMs).toBeLessThan(goFetchMs);
    expect(goFetchMs).toBeLessThan(webRefreshMs);
    expect(webRefreshMs).toBeLessThan(nextProxyMs);
    expect(nextProxyMs).toBe(360_000);
    expect(webRefreshMs).toBe(330_000);
  });
});

describe("AbortError mapping", () => {
  it("maps operation timeout to market_provider_timeout", async () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { apiRequest } = await import("./client");
    const originalFetch = global.fetch;
    global.fetch = vi.fn(() => {
      const err = new DOMException("The operation timed out.", "AbortError");
      return Promise.reject(err);
    }) as typeof fetch;

    await expect(
      apiRequest("/api/v1/instruments/resolve", {
        method: "POST",
        body: "{}",
        timeoutMs: 1,
      }),
    ).rejects.toMatchObject({
      code: "market_provider_timeout",
    });

    expect(warnSpy).toHaveBeenCalledWith(
      "market provider timeout operation=/api/v1/instruments/resolve layer=web",
    );

    warnSpy.mockRestore();
    global.fetch = originalFetch;
  });
});

describe("apiRequest null data", () => {
  it("returns null when success envelope omits data", async () => {
    const { apiRequest } = await import("./client");
    const originalFetch = global.fetch;
    global.fetch = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ code: "ok", message: "success" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    ) as typeof fetch;

    await expect(apiRequest("/api/v1/plans/p1/rebalance-executions/active")).resolves.toBeNull();

    global.fetch = originalFetch;
  });
});
