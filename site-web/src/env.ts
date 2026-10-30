import { z } from "zod";

// The single typed entry point for environment variables. Every other
// module imports `env` from here rather than reading `process.env`.
const schema = z.object({
  // Base URL of the Go backend; browsers never see it. Production has
  // no default, so a missing value fails fast instead of silently
  // calling localhost.
  GARAM_API_URL:
    process.env.NODE_ENV === "production"
      ? z.url()
      : z.url().default("http://localhost:8080"),
  // Where the console is served (ADR-0131). A signed-in visitor, and
  // every address under `/console`, is sent there. It is read when a
  // request arrives rather than when the image is built, so one image
  // serves every deployment.
  GARAM_CONSOLE_URL:
    process.env.NODE_ENV === "production"
      ? z.url()
      : z.url().default("http://console.garam.localhost:3000"),
  // Where cowork's web app is served (ADR-0135), the other app the site
  // hands a sign-in to. Read when a request arrives, as the console's.
  GARAM_COWORK_URL:
    process.env.NODE_ENV === "production"
      ? z.url()
      : z.url().default("http://cowork.garam.localhost:3100"),
});

/**
 * The validated environment. A missing value fails at import rather
 * than inside the first request that needs it.
 */
export const env = schema.parse({
  GARAM_API_URL: process.env.GARAM_API_URL,
  GARAM_CONSOLE_URL: process.env.GARAM_CONSOLE_URL,
  GARAM_COWORK_URL: process.env.GARAM_COWORK_URL,
});
