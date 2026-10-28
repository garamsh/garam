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
  // Where the site is served (ADR-0131): the landing page and the way
  // in. A visitor without a session is sent there to sign in, and
  // returned here. It is read when a request arrives, so one image
  // serves every deployment.
  GARAM_SITE_URL:
    process.env.NODE_ENV === "production"
      ? z.url()
      : z.url().default("http://garam.localhost:3001"),
  // Where cowork's web app is served (ADR-0127 §10). The sidebar links
  // to it only where it is set, so a console without cowork offers no
  // link to nowhere.
  COWORK_WEB_URL: z.url().optional(),
});

/**
 * The validated environment. A missing value fails at import rather
 * than inside the first request that needs it.
 */
export const env = schema.parse({
  GARAM_API_URL: process.env.GARAM_API_URL,
  GARAM_SITE_URL: process.env.GARAM_SITE_URL,
  COWORK_WEB_URL: process.env.COWORK_WEB_URL || undefined,
});
