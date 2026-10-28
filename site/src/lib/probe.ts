/**
 * The answer both of the app's probes give. Liveness and readiness are
 * one answer here: neither reads the API, so reaching this handler is
 * the whole of the check (ADR-0023). The body matches what the Go
 * processes' probes return, so four surfaces read the same.
 */
export function probe(): Response {
  return new Response("ok\n", {
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}
