import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { revokeInvitation } from "@/features/orgs/api/revoke-invitation";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const ID = "0f8c2a5e-9d1b-4c3a-8e7f-6a5b4c3d2e1f";

function form(invitation: string): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("invitation", invitation);
  return data;
}

describe("revokeInvitation", () => {
  it("deletes the invitation it names by id", async () => {
    const revoked: string[] = [];
    server.use(
      http.delete(
        `${API_URL}/orgs/acme/invitations/:invitation`,
        ({ params }) => {
          revoked.push(String(params.invitation));
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );

    const state = await revokeInvitation({ status: "idle" }, form(ID));

    expect(state).toEqual({ status: "revoked" });
    expect(revoked).toEqual([ID]);
  });

  it("sends nothing for what is not an id, a token among them", async () => {
    const revoked: string[] = [];
    server.use(
      http.delete(
        `${API_URL}/orgs/acme/invitations/:invitation`,
        ({ params }) => {
          revoked.push(String(params.invitation));
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );

    const state = await revokeInvitation(
      { status: "idle" },
      form("secret-token"),
    );

    expect(state).toEqual({
      status: "error",
      message: "Name the invitation to revoke.",
    });
    expect(revoked).toEqual([]);
  });

  it("says garam's refusal when the invitation was already used", async () => {
    server.use(
      http.delete(`${API_URL}/orgs/acme/invitations/${ID}`, () =>
        HttpResponse.json(
          {
            kind: "failed_precondition",
            message: "invitation already accepted",
            reason: "invitation_accepted",
          },
          { status: 409 },
        ),
      ),
    );

    const state = await revokeInvitation({ status: "idle" }, form(ID));

    expect(state).toEqual({
      status: "error",
      message:
        "This invitation has already been used. Ask an admin of the organization for a new one.",
    });
  });
});
