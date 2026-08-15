import { z } from "zod";

// The single typed entry point for environment variables. Every other
// module imports `env` from here rather than reading `process.env`.
const schema = z.object({
  // Base URL of the Go backend (`garam serve api`). The Next.js
  // rewrite in `next.config.ts` and the server-side API client both
  // target it; browsers never see it. The localhost default is a
  // development convenience only — in production a missing value must
  // fail fast rather than silently call localhost.
  GARAM_API_URL:
    process.env.NODE_ENV === "production"
      ? z.url()
      : z.url().default("http://localhost:8080"),
});

export const env = schema.parse({
  GARAM_API_URL: process.env.GARAM_API_URL,
});
