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
});

/**
 * The validated environment. A missing value fails at import rather
 * than inside the first request that needs it.
 */
export const env = schema.parse({
  GARAM_API_URL: process.env.GARAM_API_URL,
});
