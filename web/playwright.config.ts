import { defineConfig, devices } from "@playwright/test";

const BASE_URL = process.env.GARAM_E2E_BASE_URL ?? "http://localhost:3000";
const served = new URL(BASE_URL);

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
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    // The app is served as the image ships it (deploy/local/
    // Dockerfile.web): the standalone server `output: "standalone"`
    // emits, beside the static and public assets it does not carry.
    // `next start` is not that server, and warns that it does not work
    // with standalone output. The server binds HOSTNAME and PORT, so
    // both come from the address the suite reaches, never from a
    // HOSTNAME the shell happens to export.
    command: "pnpm build && pnpm start",
    env: {
      HOSTNAME: served.hostname,
      PORT: served.port || (served.protocol === "https:" ? "443" : "80"),
    },
    url: BASE_URL,
    // The subject is what this tree builds. `url` is answered by
    // anything listening on the port, so reusing took a stale `next
    // dev` or an unrelated process as the app under test.
    reuseExistingServer: false,
    timeout: 180_000,
  },
});
