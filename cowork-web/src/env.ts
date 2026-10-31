import { z } from "zod";

// The single typed entry point for environment variables. Every other
// module imports `env` from here rather than reading `process.env`.
const url = (fallback: string) =>
  process.env.NODE_ENV === "production" ? z.url() : z.url().default(fallback);

const schema = z.object({
  // cowork's server, which the pages' Server Components and Actions
  // call with the caller's cookie. Browsers never see it.
  COWORK_API_URL: url("http://localhost:8090"),
  // The platform's browser API, asked which organizations the caller
  // belongs to.
  GARAM_API_URL: url("http://localhost:8080"),
  // Where a caller signs in: the site, which hands this host a session
  // of its own (ADR-0131, ADR-0135). cowork has no sign-in page.
  GARAM_SITE_URL: url("http://garam.localhost:3001"),
  // cowork's server as the browser reaches it for a page's sync
  // WebSocket, on this app's own host, since the session cookie is
  // host-only. Read here and handed to the editor, so the bundle
  // carries no value of its own.
  COWORK_SYNC_URL: url("ws://localhost:8090"),
});

/**
 * The validated environment. A missing value fails at import rather
 * than inside the first request that needs it.
 */
export const env = schema.parse({
  COWORK_API_URL: process.env.COWORK_API_URL,
  GARAM_API_URL: process.env.GARAM_API_URL,
  GARAM_SITE_URL: process.env.GARAM_SITE_URL,
  COWORK_SYNC_URL: process.env.COWORK_SYNC_URL,
});
