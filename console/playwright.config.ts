import { defineConfig, devices } from "@playwright/test";

// The console and the site on two hosts (ADR-0131), the shape
// a deployment has: a browser resolves every `.localhost` name to this
// machine, and each host holds a host-only session of its own.
const BASE_URL =
  process.env.GARAM_E2E_BASE_URL ?? "http://console.garam.localhost:3000";
const SITE_URL =
  process.env.GARAM_E2E_SITE_URL ?? "http://garam.localhost:3001";
const served = new URL(BASE_URL);
const site = new URL(SITE_URL);

/** The port a URL is served on, its scheme's where it names none. */
function portOf(url: URL): string {
  return url.port || (url.protocol === "https:" ? "443" : "80");
}

/**
 * The e2e suite drives the built app against the real backend: the Go
 * API, the authorization engine, and dockerised Postgres. See the web
 * section of the repository README for the run command.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  // Measured on the 12-core gate host (#1528): from 3 workers up, a
  // loaded host timed out the 30 s budget, and at 2 none did. On a
  // 4-core hosted runner 2 is the default.
  workers: 2,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
  },
  // Desktop runs every spec but the mobile ones, and the phone-width
  // project runs only those: the shell's sidebar is a sheet there, and
  // the desktop specs drive it as a column.
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
  // Both apps: the console is what this suite tests, and the site is
  // where its visitors sign in. Each is served as the image ships it
  // (deploy/local/Dockerfile.web): the standalone server `output:
  // "standalone"` emits, beside the static and public assets it does
  // not carry. `next start` is not that server, and warns that it does
  // not work with standalone output. Each binds the loopback address
  // the `.localhost` names resolve to, and its readiness is asked
  // there: Node does not resolve those names the way a browser does.
  webServer: [
    {
      command: "pnpm build && pnpm start",
      env: {
        HOSTNAME: "127.0.0.1",
        PORT: portOf(served),
        GARAM_SITE_URL: SITE_URL,
      },
      url: `http://127.0.0.1:${portOf(served)}/healthz`,
      // The subject is what this tree builds. `url` is answered by
      // anything listening on the port, so reusing took a stale `next
      // dev` or an unrelated process as the app under test.
      reuseExistingServer: false,
      timeout: 180_000,
    },
    {
      command: "pnpm --dir ../site build && pnpm --dir ../site start",
      env: {
        HOSTNAME: "127.0.0.1",
        PORT: portOf(site),
        GARAM_CONSOLE_URL: BASE_URL,
      },
      url: `http://127.0.0.1:${portOf(site)}/healthz`,
      reuseExistingServer: false,
      timeout: 180_000,
    },
  ],
});
