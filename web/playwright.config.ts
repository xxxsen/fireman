import { defineConfig } from "@playwright/test";

const inheritedNoProxy = process.env.NO_PROXY || process.env.no_proxy;
const noProxy = [inheritedNoProxy, "127.0.0.1,localhost"].filter(Boolean).join(",");
process.env.NO_PROXY = noProxy;
process.env.no_proxy = noProxy;
delete process.env.NO_COLOR;

const port = Number(process.env.FIREMAN_E2E_PORT ?? 3130);
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
const reuseExistingServer = process.env.FIREMAN_E2E_REUSE_SERVER === "1";

export default defineConfig({
  testDir: "./e2e",
  outputDir: "test-results",
  reporter: [["list"], ["html", { open: "never" }]],
  fullyParallel: true,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    viewport: { width: 1440, height: 900 },
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    launchOptions: executablePath ? { executablePath } : undefined,
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: {
    command: `npm run dev -- --hostname 127.0.0.1 --port ${port}`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer,
    timeout: 120_000,
    env: {
      API_PROXY_TARGET: "http://127.0.0.1:1",
      NEXT_DIST_DIR: ".next/e2e",
    },
  },
});
