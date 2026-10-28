import { env } from "@/env";

/**
 * The site's address for `path` (ADR-0131). Signing in and registering
 * are the site's, on its own origin, so a link or a redirect to either
 * names that origin rather than a path on this one.
 */
export function siteUrl(path: string): string {
  return new URL(path, env.GARAM_SITE_URL).toString();
}
