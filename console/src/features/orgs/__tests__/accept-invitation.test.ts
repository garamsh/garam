import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { acceptInvitation } from "@/features/orgs/api/accept-invitation";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function form(entries: Record<string, string>): FormData {
  const data = new FormData();
  for (const [key, value] of Object.entries(entries)) data.set(key, value);
  return data;
}

describe("acceptInvitation", () => {
  it("accepts the token and lands in the organization it named", async () => {
    server.use(
      http.post(`${API_URL}/invitations/secret-token/accept`, () =>
        HttpResponse.json({
          userGrn: "grn:root:default:user:bob",
          role: "member",
          since: "2026-01-02T03:04:05Z",
        }),
      ),
    );

    await expect(
      acceptInvitation({ status: "idle" }, form({ token: "secret-token" })),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme");
  });

  it.each([
    [
      404,
      "not_found",
      "This invitation link isn't valid. Check that you opened the whole link, or ask for a new invitation.",
    ],
    [
      409,
      "failed_precondition",
      "This invitation can no longer be used: it was already accepted or it has expired. Ask an admin of the organization for a new one.",
    ],
    [409, "already_exists", "You're already a member of this organization."],
  ])(
    "says why a %i %s refusal stopped the join, in its own words",
    async (status, kind, message) => {
      server.use(
        http.post(`${API_URL}/invitations/spent/accept`, () =>
          HttpResponse.json({ kind, message: "server text" }, { status }),
        ),
      );

      const state = await acceptInvitation(
        { status: "idle" },
        form({ token: "spent" }),
      );

      expect(state).toEqual({ status: "error", message });
    },
  );
});
