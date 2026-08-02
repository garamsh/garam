import { defineConfig, devices } from "@playwright/test";

const BASE_URL = process.env.GARAM_E2E_BASE_URL ?? "http://localhost:3000";

/**
 * The e2e suite drives the built app against the real backend: the Go
 * API, the authorization engine, and dockerised Postgres. See the web
 * section of the repository README for the run command.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
  ],
  webServer: {
    command: "pnpm build && pnpm start",
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
});
