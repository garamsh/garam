import { defineConfig, devices } from "@playwright/test";

/**
 * The e2e suite drives the built app and the built cowork binary
 * against the platform `make backend-up` starts: its API, its
 * authorization engine and its Postgres, where cowork keeps a database
 * of its own. `make cowork-web-e2e` supplies every variable below.
 */
const required = (name: string): string => {
  const v = process.env[name];
  if (!v)
    throw new Error(
      `${name} is unset — run the suite through \`make cowork-web-e2e\``,
    );
  return v;
};

const WEB_URL = "http://localhost:3100";
const COWORK_PORT = "8091";

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
        COWORK_LISTEN_ADDR: `localhost:${COWORK_PORT}`,
        COWORK_WEB_ORIGINS: new URL(WEB_URL).host,
      },
      url: `http://localhost:${COWORK_PORT}/readyz`,
      reuseExistingServer: false,
      timeout: 60_000,
    },
    {
      // The standalone server the image would ship, not `next start`.
      command: "pnpm build && pnpm start",
      env: {
        HOSTNAME: "localhost",
        PORT: new URL(WEB_URL).port,
        COWORK_API_URL: `http://localhost:${COWORK_PORT}`,
        COWORK_SYNC_URL: `ws://localhost:${COWORK_PORT}`,
        GARAM_API_URL: required("GARAM_API_URL"),
        GARAM_CONSOLE_URL: "http://localhost:3000",
      },
      url: WEB_URL,
      reuseExistingServer: false,
      timeout: 180_000,
    },
  ],
});
