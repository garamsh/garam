import { env } from "@/env";

/**
 * The console's address for `path`, a path under `/console` that was
 * already checked as a return path. The console is another origin
 * (ADR-0131), so a link or a redirect to it names that origin rather
 * than a path on this one.
 */
export function consoleUrl(path: string): string {
  return new URL(path, env.GARAM_CONSOLE_URL).toString();
}
