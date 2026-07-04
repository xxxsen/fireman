// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

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
      apiRequest("/api/v1/market-assets/sync", {
        method: "POST",
        body: "{}",
        timeoutMs: 1,
      }),
    ).rejects.toMatchObject({
      code: "market_provider_timeout",
    });

    expect(warnSpy).toHaveBeenCalledWith(
      "market provider timeout operation=/api/v1/market-assets/sync layer=web",
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
