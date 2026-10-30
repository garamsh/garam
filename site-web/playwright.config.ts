import { defineConfig, devices } from "@playwright/test";

// The site and the console on two hosts (ADR-0131), the shape
// a deployment has: a browser resolves every `.localhost` name to this
// machine, and each host holds a host-only session of its own.
const BASE_URL =
  process.env.GARAM_E2E_BASE_URL ?? "http://garam.localhost:3001";
const CONSOLE_URL =
  process.env.GARAM_E2E_CONSOLE_URL ?? "http://console.garam.localhost:3000";
const served = new URL(BASE_URL);
const consoleApp = new URL(CONSOLE_URL);

/** The port a URL is served on, its scheme's where it names none. */
function portOf(url: URL): string {
  return url.port || (url.protocol === "https:" ? "443" : "80");
}

/**
 * The e2e suite drives the built site against the real backend — the
 * Go API, the authorization engine, and dockerised Postgres — and the
 * console a signed-in visitor is handed to.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  // The console's measurement (#1528): from 3 workers up, a loaded
  // gate host timed out the 30 s budget, and at 2 none did.
  workers: 2,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
  },
  // Desktop runs every spec but the mobile ones, and the phone-width
  // project runs only those, as in the console's suite.
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
      testIgnore: /\.mobile\.spec\.ts$/,
    },
    {
      name: "mobile",
      use: {
        browserName: "chromium",
        viewport: { width: 390, height: 844 },
        deviceScaleFactor: 3,
        isMobile: true,
        hasTouch: true,
      },
      testMatch: /\.mobile\.spec\.ts$/,
    },
  ],
  // Both apps, each served as the image ships it (deploy/local/
  // Dockerfile.web): the standalone server beside the static and public
  // assets it does not carry. Each binds the loopback address the
  // `.localhost` names resolve to, and its readiness is asked there:
  // Node does not resolve those names the way a browser does.
  webServer: [
    {
      command: "pnpm build && pnpm start",
      env: {
        HOSTNAME: "127.0.0.1",
        PORT: portOf(served),
        GARAM_CONSOLE_URL: CONSOLE_URL,
        // Required of the site; this suite follows no handoff to cowork.
        GARAM_COWORK_URL: "http://cowork.garam.localhost:3100",
      },
      url: `http://127.0.0.1:${portOf(served)}/healthz`,
      // The subject is what this tree builds. `url` is answered by
      // anything listening on the port, so reusing took a stale server
      // as the app under test.
      reuseExistingServer: false,
      timeout: 180_000,
    },
    {
      command:
        "pnpm --dir ../platform-web build && pnpm --dir ../platform-web start",
      env: {
        HOSTNAME: "127.0.0.1",
        PORT: portOf(consoleApp),
        GARAM_SITE_URL: BASE_URL,
      },
      url: `http://127.0.0.1:${portOf(consoleApp)}/healthz`,
      reuseExistingServer: false,
      timeout: 180_000,
    },
  ],
});
