import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { getSession } from "@/features/auth/api/get-session";
import { API_URL, alice, signedOut } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

describe("getSession", () => {
  it("returns the user behind the session cookie", async () => {
    await expect(getSession()).resolves.toEqual({
      status: "signed-in",
      user: alice,
    });
  });

  it("answers signed-out when the backend rejects the session", async () => {
    server.use(...signedOut());

    await expect(getSession()).resolves.toEqual({ status: "signed-out" });
  });

  it("answers unreachable when the API never answered", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await expect(getSession()).resolves.toEqual({ status: "unreachable" });
  });

  it("propagates failures that are not an absent session", async () => {
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          { kind: "internal", message: "boom" },
          { status: 500 },
        ),
      ),
    );

    await expect(getSession()).rejects.toThrow("boom");
  });
});
