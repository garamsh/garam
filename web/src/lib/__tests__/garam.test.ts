import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { z } from "zod";

import {
  GaramApiError,
  GaramContractError,
  GaramUnreachableError,
  garamFetch,
} from "@/lib/garam";
import { API_URL, alice } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const User = z.object({ grn: z.string(), nickname: z.string() });

describe("garamFetch", () => {
  it("returns the body the backend sent", async () => {
    await expect(garamFetch("/me", User)).resolves.toEqual(alice);
  });

  it("refuses a body the parser does not describe", async () => {
    server.use(
      http.get(`${API_URL}/me`, () => HttpResponse.json({ grn: alice.grn })),
    );

    await expect(garamFetch("/me", User)).rejects.toBeInstanceOf(
      GaramContractError,
    );

    // The control: the same read against the body the backend answers
    // with is accepted, so the refusal above is the missing field and
    // not a parser that refuses everything.
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.json(alice)));

    await expect(garamFetch("/me", User)).resolves.toEqual(alice);
  });

  it("refuses a body that is not JSON at all", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.text("not json")));

    await expect(garamFetch("/me", z.unknown())).rejects.toBeInstanceOf(
      GaramContractError,
    );

    // The control: `z.unknown()` accepts whatever the backend sends, so
    // the refusal above is the body not being JSON rather than the
    // parser refusing.
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.json(alice)));

    await expect(garamFetch("/me", z.unknown())).resolves.toEqual(alice);
  });

  it("tells an outage apart from a refusal, whichever way the outage arrives", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await expect(garamFetch("/me", User)).rejects.toBeInstanceOf(
      GaramUnreachableError,
    );

    // A dependency garam needs being unreachable arrives as a status
    // and not as a connection failure (`api/openapi.yaml` §info), and
    // its body names no cause: it is the same outage to a caller.
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          { kind: "unavailable", message: "internal error" },
          { status: 503 },
        ),
      ),
    );

    await expect(garamFetch("/me", User)).rejects.toBeInstanceOf(
      GaramUnreachableError,
    );

    // The control: a status garam decided for itself is a refusal the
    // caller can act on, so the two answers do not collapse into one.
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await expect(garamFetch("/me", User)).rejects.toBeInstanceOf(GaramApiError);
    await expect(garamFetch("/me", User)).rejects.not.toBeInstanceOf(
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

    await expect(garamFetch("/me", User)).rejects.toMatchObject({
      status: 401,
      message: "no session",
    });
  });
});
