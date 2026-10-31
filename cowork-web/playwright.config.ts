import { defineConfig, devices } from "@playwright/test";

/**
 * The e2e suite drives the built app, the built cowork binary and the
 * site against the platform `make backend-up` starts: its API, its
 * authorization engine and its Postgres, where cowork keeps a database
 * of its own. `make cowork-web-e2e` supplies every variable below.
 *
 * cowork-web and the site sit on two hosts, the shape a deployment has
 * (ADR-0131): a browser resolves every `.localhost` name to this
 * machine. cowork's server takes the app's host on a port of its own
 * rather than a gateway's path, since a host-only cookie ignores the
 * port, so nothing here exercises the path split (ADR-0135).
 */
const required = (name: string): string => {
  const v = process.env[name];
  if (!v)
    throw new Error(
      `${name} is unset — run the suite through \`make cowork-web-e2e\``,
    );
  return v;
};

const WEB_HOST = "cowork.garam.localhost";
const WEB_URL = `http://${WEB_HOST}:3100`;
const SITE_URL = "http://garam.localhost:3001";
const COWORK_PORT = "8091";
const USERCONTENT_URL = "http://localhost:8093";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  workers: 2,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  use: { baseURL: WEB_URL, trace: "on-first-retry" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      // The binary as `go build` produces it, migrating its database
      // and then serving, as a deployment's two processes would.
      command: `${required("COWORK_BIN")} migrate && exec ${required("COWORK_BIN")} serve`,
      env: {
        COWORK_DATABASE_URL: required("COWORK_E2E_DATABASE_URL"),
        COWORK_PLATFORM_URL: required("GARAM_API_URL"),
        COWORK_MIGRATIONS_DIR: required("COWORK_MIGRATIONS_DIR"),
        COWORK_LISTEN_ADDR: `127.0.0.1:${COWORK_PORT}`,
        COWORK_WEB_ORIGINS: new URL(WEB_URL).host,
        // Artifacts kept in memory, and served from an origin of their
        // own: another port, as development runs it (ADR-0127 §8).
        COWORK_BLOB_STORE: "memory",
        COWORK_USERCONTENT_ORIGIN: USERCONTENT_URL,
        COWORK_USERCONTENT_ADDR: `localhost:${new URL(USERCONTENT_URL).port}`,
        // A key for this suite's server alone, which signs nothing a
        // deployment trusts.
        COWORK_USERCONTENT_SECRET: "e2e-only-usercontent-signing-key-32b",
      },
      url: `http://127.0.0.1:${COWORK_PORT}/readyz`,
      reuseExistingServer: false,
      timeout: 60_000,
    },
    {
      // The standalone server the image would ship, not `next start`.
      command: "pnpm build && pnpm start",
      env: {
        HOSTNAME: "127.0.0.1",
        PORT: new URL(WEB_URL).port,
        COWORK_API_URL: `http://127.0.0.1:${COWORK_PORT}`,
        COWORK_SYNC_URL: `ws://${WEB_HOST}:${COWORK_PORT}`,
        GARAM_API_URL: required("GARAM_API_URL"),
        GARAM_SITE_URL: SITE_URL,
      },
      // Readiness is asked on the loopback address: Node does not
      // resolve `.localhost` names the way a browser does.
      url: `http://127.0.0.1:${new URL(WEB_URL).port}/healthz`,
      reuseExistingServer: false,
      timeout: 180_000,
    },
    {
      // The site, where a visitor signs in and is handed to this app.
      command: "pnpm --dir ../site-web build && pnpm --dir ../site-web start",
      env: {
        HOSTNAME: "127.0.0.1",
        PORT: new URL(SITE_URL).port,
        GARAM_API_URL: required("GARAM_API_URL"),
        GARAM_CONSOLE_URL: "http://console.garam.localhost:3000",
        GARAM_COWORK_URL: WEB_URL,
      },
      url: `http://127.0.0.1:${new URL(SITE_URL).port}/healthz`,
      reuseExistingServer: false,
      timeout: 180_000,
    },
  ],
});
