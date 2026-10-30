/**
 * The answer both of the app's probes give. Neither reads cowork's
 * server or the platform, so reaching this handler is the whole of the
 * check (ADR-0023), and the body matches what the other apps' probes
 * return.
 */
export function probe(): Response {
  return new Response("ok\n", {
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}
