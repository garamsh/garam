import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { logOut } from "@/features/auth/api/log-out";
import { SESSION_COOKIE } from "@garam/platform-client/session";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// The cookie jar exists only inside a request; this one records what
// the action deleted from it.
const deleted = vi.hoisted(() => vi.fn());

vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) => ({ name, value: "test-session-token" }),
      delete: deleted,
    }),
}));

// `redirect()` throws a control-flow error the router catches; the
// test only needs to observe where it was sent.
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("logOut", () => {
  // Each test asserts what its own run deleted, not an earlier test's.
  beforeEach(() => deleted.mockClear());

  it("clears the session on the domain garam scoped it to", async () => {
    // A plain Response: msw's jar refuses a cookie for another domain.
    server.use(
      http.post(
        `${API_URL}/auth/logout`,
        () =>
          new Response(null, {
            status: 204,
            headers: {
              "set-cookie":
                "garam_session=; Path=/; Domain=garam.test; Max-Age=0; HttpOnly; Secure; SameSite=Lax",
            },
          }),
      ),
    );

    await expect(logOut()).rejects.toThrow("NEXT_REDIRECT:/login");
    // ADR-0131: a host-only delete leaves the parent domain's cookie,
    // and the browser signed in on every other host.
    expect(deleted).toHaveBeenCalledWith({
      name: SESSION_COOKIE,
      path: "/",
      domain: "garam.test",
    });
  });

  it("logs the browser out when the backend already rejects the session", async () => {
    server.use(
      http.post(`${API_URL}/auth/logout`, () =>
        HttpResponse.json(
          { kind: "unauthenticated", message: "no session" },
          { status: 401 },
        ),
      ),
    );

    await expect(logOut()).rejects.toThrow("NEXT_REDIRECT:/login");
    expect(deleted).toHaveBeenCalledWith({ name: SESSION_COOKIE, path: "/" });
  });

  it("clears the cookie while propagating a failure that is not an expired session", async () => {
    server.use(
      http.post(`${API_URL}/auth/logout`, () =>
        HttpResponse.json(
          { kind: "internal", message: "boom" },
          { status: 500 },
        ),
      ),
    );

    await expect(logOut()).rejects.toThrow("boom");
    expect(deleted).toHaveBeenCalledWith({ name: SESSION_COOKIE, path: "/" });
  });
});
