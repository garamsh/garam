import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { listProviders } from "@/features/auth";
import { API_URL } from "@/testing/mocks/handlers";
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
