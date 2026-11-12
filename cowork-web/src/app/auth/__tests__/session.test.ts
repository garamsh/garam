// @vitest-environment node
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GET } from "@/app/auth/session/route";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// The browser's cookies, which a test sets.
const browser = vi.hoisted(() => ({
  session: undefined as string | undefined,
}));
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) =>
        browser.session === undefined
          ? undefined
          : { name, value: browser.session },
    }),
}));
vi.mock("server-only", () => ({}));

beforeEach(() => {
  browser.session = "live-session";
});

describe("asking whether the session stands", () => {
  it("answers 204 for a session the platform accepts, presenting the caller's cookie", async () => {
    const sent: (string | null)[] = [];
    server.use(
      http.get(`${API_URL}/me`, ({ request }) => {
        sent.push(request.headers.get("cookie"));
        return HttpResponse.json({ grn: "grn:root/iam/user/ada" });
      }),
    );

    expect((await GET()).status).toBe(204);
    expect(sent).toEqual(["garam_session=live-session"]);
  });

  it("answers 401 for a session the platform refuses", async () => {
    server.use(
      http.get(`${API_URL}/me`, () => new HttpResponse(null, { status: 401 })),
    );

    expect((await GET()).status).toBe(401);
  });

  it("answers 401 without asking the platform when the browser holds no session", async () => {
    browser.session = undefined;
    const asked = vi.fn();
    server.use(
      http.get(`${API_URL}/me`, () => {
        asked();
        return HttpResponse.json({});
      }),
    );

    expect((await GET()).status).toBe(401);
    expect(asked).not.toHaveBeenCalled();
  });

  it("throws a platform failure rather than reading it as a refusal", async () => {
    server.use(
      http.get(`${API_URL}/me`, () => new HttpResponse(null, { status: 503 })),
    );

    await expect(GET()).rejects.toThrow("503");
  });
});
