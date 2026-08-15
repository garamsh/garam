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
  it("accepts the token and lands on the organization list", async () => {
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
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs");
  });

  it.each([404, 409])(
    "surfaces the error for a missing or spent token (%i)",
    async (status) => {
      server.use(
        http.post(`${API_URL}/invitations/spent/accept`, () =>
          HttpResponse.json(
            { kind: "conflict", message: "invitation already accepted" },
            { status },
          ),
        ),
      );

      const state = await acceptInvitation(
        { status: "idle" },
        form({ token: "spent" }),
      );

      expect(state).toEqual({
        status: "error",
        message: "invitation already accepted",
      });
    },
  );
});
