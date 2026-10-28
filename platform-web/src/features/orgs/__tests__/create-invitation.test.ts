import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createInvitation } from "@/features/orgs/api/create-invitation";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function form(entries: Record<string, string>): FormData {
  const data = new FormData();
  for (const [key, value] of Object.entries(entries)) data.set(key, value);
  return data;
}

const invitation = {
  token: "secret-token",
  orgGrn: "grn:root:default:org:acme",
  role: "member",
  expiresAt: "2026-01-09T03:04:05Z",
};

describe("createInvitation", () => {
  it("posts the role and returns the invitation token", async () => {
    let body: unknown;
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(invitation, { status: 201 });
      }),
    );

    const state = await createInvitation(
      { status: "idle" },
      form({ org: "acme", role: "member" }),
    );

    expect(state).toEqual({ status: "created", token: "secret-token" });
    expect(body).toEqual({ role: "member" });
  });

  it("rejects a role the backend does not offer", async () => {
    const state = await createInvitation(
      { status: "idle" },
      form({ org: "acme", role: "owner" }),
    );

    expect(state).toEqual({
      status: "error",
      message: "Choose a role for the invitation.",
    });
  });

  it("surfaces a 403 from the backend", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "requires the admin role" },
          { status: 403 },
        ),
      ),
    );

    const state = await createInvitation(
      { status: "idle" },
      form({ org: "acme", role: "admin" }),
    );

    expect(state).toEqual({
      status: "error",
      message: REFUSAL_COPY.permissionDenied,
    });
  });
});
