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
  // Where a caller signs in: the console. cowork has no sign-in page
  // of its own; the platform's session is the credential (ADR-0127).
  GARAM_CONSOLE_URL: url("http://localhost:3000"),
  // cowork's server as the browser reaches it for a page's sync
  // WebSocket. Read here and handed to the editor, so the bundle
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
  GARAM_CONSOLE_URL: process.env.GARAM_CONSOLE_URL,
  COWORK_SYNC_URL: process.env.COWORK_SYNC_URL,
});
