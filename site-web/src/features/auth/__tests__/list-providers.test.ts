import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { listProviders } from "@/features/auth";
import { API_URL, signedOut } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

describe("listProviders", () => {
  it("returns the names the backend registered", async () => {
    server.use(
      http.get(`${API_URL}/auth/providers`, () =>
        HttpResponse.json(["google"]),
      ),
    );

    await expect(listProviders()).resolves.toEqual(["google"]);
  });

  // The subject here is the signed-out fixture rather than the read:
  // `/auth/providers` is public, and a fixture refusing it would fail a
  // page that is correct.
  it("returns the names when the caller holds no session", async () => {
    server.use(...signedOut());

    await expect(listProviders()).resolves.toEqual(["google"]);
  });

  it("returns no names when the backend has registered none", async () => {
    server.use(
      http.get(`${API_URL}/auth/providers`, () => HttpResponse.json([])),
    );

    await expect(listProviders()).resolves.toEqual([]);
  });

  it("returns no names when the API never answered", async () => {
    server.use(
      http.get(`${API_URL}/auth/providers`, () => HttpResponse.error()),
    );

    await expect(listProviders()).resolves.toEqual([]);
  });

  it("returns no names when the backend refuses the read", async () => {
    server.use(
      http.get(`${API_URL}/auth/providers`, () =>
        HttpResponse.json(
          { kind: "internal", message: "boom" },
          { status: 500 },
        ),
      ),
    );

    await expect(listProviders()).resolves.toEqual([]);
  });
});
