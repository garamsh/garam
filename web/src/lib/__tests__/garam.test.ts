import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { z } from "zod";

import {
  GaramApiError,
  GaramContractError,
  GaramUnreachableError,
  garamFetch,
  refusal,
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

  it("carries the status and the kind the backend refused with", async () => {
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
      kind: "unauthenticated",
      message: "no session",
    });
  });

  it("carries the reason and pointer the backend refused with", async () => {
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          {
            kind: "invalid_argument",
            message: "policy document: Effect must be Allow or Deny",
            reason: "policy_document_effect_invalid",
            path: "/Statement/0/Effect",
          },
          { status: 400 },
        ),
      ),
    );

    await expect(garamFetch("/me", User)).rejects.toMatchObject({
      status: 400,
      kind: "invalid_argument",
      reason: {
        code: "policy_document_effect_invalid",
        path: "/Statement/0/Effect",
      },
    });
  });

  it("keeps the status of an answer that carries no error body", async () => {
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.text("<html>bad gateway</html>", {
          status: 502,
          statusText: "Bad Gateway",
        }),
      ),
    );

    await expect(garamFetch("/me", User)).rejects.toMatchObject({
      status: 502,
      kind: "unknown",
      message: "502 Bad Gateway",
    });
  });
});

// The expected failures a Server Action returns rather than throws
// (stack-nextjs.md §Errors — returned, thrown, or caught): a refusal and
// an outage come back as the state a form renders, in a sentence
// written for a person, and anything else — a contract garam broke, a
// bug in this application — is thrown on as the very error it was.
describe("refusal", () => {
  it("writes a refusal's sentence from its status and kind, not its words", () => {
    // One message under two kinds reads differently, and two messages
    // under one kind read the same: the words are not what is matched.
    const denied = refusal(
      new GaramApiError(403, "org not found", "permission_denied"),
    );
    const missing = refusal(
      new GaramApiError(404, "org not found", "not_found"),
    );
    const missingTeam = refusal(
      new GaramApiError(404, "group not found", "not_found"),
    );

    expect(denied.message).toBe(
      "You don't have permission to do this. An owner or admin of the organization can grant it.",
    );
    expect(missing.message).toBe(
      "This no longer exists. It may have been deleted — reload the page to see what's there now.",
    );
    expect(missingTeam).toEqual(missing);
    expect(missing.message).not.toContain("org not found");
  });

  it("reads a pair for the operation that asked where it means something particular there", () => {
    const notFound = new GaramApiError(
      404,
      "invitation not found",
      "not_found",
    );

    expect(refusal(notFound, "acceptInvitation").message).toBe(
      "This invitation link isn't valid. Check that you opened the whole link, or ask for a new invitation.",
    );
    // The control: the same answer to an operation with no sentence of
    // its own for it reads the generic one.
    expect(refusal(notFound, "createPolicy").message).toBe(
      refusal(notFound).message,
    );
  });

  it("names the rules a policy document breaks, in plain words", () => {
    const refused = new GaramApiError(
      400,
      "policy document: statement : Effect must be Allow or Deny",
      "invalid_argument",
    );

    const { message } = refusal(refused, "addPolicyVersion");

    expect(message).toMatch(
      /an Effect of Allow or Deny, an Action and a Resource/,
    );
    expect(message).not.toContain("policy document:");
  });

  it("reads a documented reason before the status and kind, naming the place it points at", () => {
    const refused = (reason: string, path?: string) =>
      refusal(
        new GaramApiError(400, "policy document: rule", "invalid_argument", {
          code: reason,
          path,
        }),
        "addPolicyVersion",
      ).message;

    expect(
      refused("policy_document_effect_invalid", "/Statement/1/Effect"),
    ).toBe("Statement 2's Effect must be Allow or Deny.");
    expect(
      refused("policy_document_resource_missing", "/Statement/0/Resource"),
    ).toBe("Statement 1 needs at least one Resource.");
    expect(
      refused("policy_document_unknown_key", "/Statement/0/Bogus~1x"),
    ).toMatch(/^Statement 1's Bogus\/x isn't part of the policy format\./);
    expect(refused("policy_document_wrong_type", "/Statement/0/Action/1")).toBe(
      "Statement 1's Action, item 2 has the wrong type of value.",
    );
    expect(refused("policy_document_version_missing", "/Version")).toBe(
      "The document needs a Version.",
    );
    // The control: the same status and kind with no reason read the
    // sentence that names every rule, so the ones above are the
    // reason's.
    expect(
      refusal(
        new GaramApiError(400, "policy document: rule", "invalid_argument"),
        "addPolicyVersion",
      ).message,
    ).toMatch(/needs a Version and at least one statement/);
  });

  it("tells a used invitation from an expired one by reason", () => {
    const spent = (code: string) =>
      refusal(
        new GaramApiError(409, "invitation", "failed_precondition", { code }),
        "acceptInvitation",
      ).message;

    expect(spent("invitation_accepted")).toBe(
      "This invitation has already been used. Ask an admin of the organization for a new one.",
    );
    expect(spent("invitation_expired")).toBe(
      "This invitation has expired. Ask an admin of the organization to send you a new one.",
    );
  });

  it("reads a reason it does not know, or one named like an object's own key, by status and kind", () => {
    for (const code of ["a_code_added_later", "constructor", "toString"]) {
      expect(
        refusal(
          new GaramApiError(409, "invitation", "failed_precondition", { code }),
          "acceptInvitation",
        ).message,
      ).toMatch(/^This invitation can no longer be used/);
    }
  });

  it("quotes the server's text only inside a generic sentence for a refusal nothing maps", () => {
    expect(
      refusal(
        new GaramApiError(409, "the team is not empty", "failed_precondition"),
      ),
    ).toEqual({
      status: "error",
      message:
        "garam couldn't complete this request. It answered: “the team is not empty”",
    });
  });

  it("says an outage in its own sentence", () => {
    expect(refusal(new GaramUnreachableError())).toEqual({
      status: "error",
      message: "garam can't be reached right now. Try again in a moment.",
    });
  });

  it("throws anything else on unchanged", () => {
    const broken = new GaramContractError("/orgs");
    const bug = new TypeError("undefined is not a function");

    expect(thrownBy(broken)).toBe(broken);
    expect(thrownBy(bug)).toBe(bug);
    expect(thrownBy("not even an Error")).toBe("not even an Error");
  });

  // What refusal threw, compared by identity: a rethrow that wrapped or
  // copied the error would lose its stack and its class.
  function thrownBy(error: unknown): unknown {
    try {
      refusal(error);
    } catch (caught) {
      return caught;
    }
    return "nothing was thrown";
  }
});
