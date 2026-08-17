import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { GaramApiError, GaramUnreachableError, garamFetch } from "@/lib/garam";
import { API_URL, alice } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

describe("garamFetch", () => {
  it("returns the body the backend sent", async () => {
    await expect(garamFetch("/me")).resolves.toEqual(alice);
  });

  it("tells a connection failure apart from a refusal", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await expect(garamFetch("/me")).rejects.toBeInstanceOf(
      GaramUnreachableError,
    );

    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          { kind: "unavailable", message: "no" },
          { status: 503 },
        ),
      ),
    );

    await expect(garamFetch("/me")).rejects.toBeInstanceOf(GaramApiError);
    await expect(garamFetch("/me")).rejects.not.toBeInstanceOf(
      GaramUnreachableError,
    );
  });

  it("carries the status the backend refused with", async () => {
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          { kind: "unauthenticated", message: "no session" },
          { status: 401 },
        ),
      ),
    );

    await expect(garamFetch("/me")).rejects.toMatchObject({
      status: 401,
      message: "no session",
    });
  });
});
