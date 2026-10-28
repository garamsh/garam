import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { GET as healthz } from "@/app/healthz/route";
import { GET as readyz } from "@/app/readyz/route";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// One decision mounted at two segments (ADR-0023), so one test file:
// the two answer the same, and a difference between them would be the
// defect.
const probes = [
  { path: "/healthz", handler: healthz },
  { path: "/readyz", handler: readyz },
];

/** Counts the requests that leave the process while it is installed. */
function recordOutbound() {
  let count = 0;
  server.events.on("request:start", () => {
    count += 1;
  });
  return () => count;
}

afterEach(() => server.events.removeAllListeners());

describe.each(probes)("GET $path", ({ handler }) => {
  it("answers ok", async () => {
    const response = handler();

    expect(response.status).toBe(200);
    await expect(response.text()).resolves.toBe("ok\n");
  });

  it("answers ok with the API unreachable, having asked it nothing", async () => {
    server.use(http.all("*", () => HttpResponse.error()));
    const outbound = recordOutbound();

    const response = handler();

    expect(response.status).toBe(200);
    await expect(response.text()).resolves.toBe("ok\n");
    expect(outbound()).toBe(0);

    // Control for that zero: a read of the API does happen, does fail,
    // and is counted — so the zero is the probe asking nothing rather
    // than a recorder that never fired.
    await expect(fetch(`${API_URL}/me`)).rejects.toThrow();
    expect(outbound()).toBe(1);
  });
});
