import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { addGroupMember } from "@/features/orgs/api/add-group-member";
import { removeGroupMember } from "@/features/orgs/api/remove-group-member";
import { API_URL, alice, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** The body an addition posts, parsed rather than asserted. */
const MemberRef = z.object({ memberGrn: z.string() });

const MEMBERS = `${API_URL}/orgs/acme/groups/eng/members`;

function form(member: string): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("group", "eng");
  data.set("member", member);
  return data;
}

describe("addGroupMember", () => {
  it.each([
    ["a person", alice.grn],
    ["an agent", reportedAgent.grn],
  ])("adds %s to the one team named", async (_kind, grn) => {
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      addGroupMember({ status: "idle" }, form(grn)),
    ).resolves.toEqual({ status: "idle" });
    expect(posted).toEqual([{ memberGrn: grn }]);
  });

  it("says a member added since the page was read is already there", async () => {
    // garam holds Alice already and nobody else.
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        const { memberGrn } = MemberRef.parse(await request.json());
        return memberGrn === alice.grn
          ? HttpResponse.json(
              { kind: "already_exists", message: "already exists" },
              { status: 409 },
            )
          : new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      addGroupMember({ status: "idle" }, form(alice.grn)),
    ).resolves.toEqual({
      status: "error",
      message: "Already a member of this team. The list now shows it.",
    });

    // The control: a member garam does not hold yet is added, so the
    // error above is the conflict and not every addition.
    await expect(
      addGroupMember({ status: "idle" }, form(reportedAgent.grn)),
    ).resolves.toEqual({ status: "idle" });
  });

  it("renders any other refusal in garam's words", async () => {
    let refusing = true;
    server.use(
      http.post(MEMBERS, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : new HttpResponse(null, { status: 204 }),
      ),
    );

    await expect(
      addGroupMember({ status: "idle" }, form(alice.grn)),
    ).resolves.toEqual({ status: "error", message: "permission denied" });

    // The control: the same addition garam accepts succeeds.
    refusing = false;
    await expect(
      addGroupMember({ status: "idle" }, form(alice.grn)),
    ).resolves.toEqual({ status: "idle" });
  });

  it("refuses a choice that names no identity, without asking garam", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(addGroupMember({ status: "idle" }, form(""))).resolves.toEqual(
      { status: "error", message: "Choose a person or an agent to add." },
    );
    expect(posted).toEqual([]);

    // The control: the same form naming an identity reaches garam.
    await expect(
      addGroupMember({ status: "idle" }, form(alice.grn)),
    ).resolves.toEqual({ status: "idle" });
    expect(posted).toEqual([{ memberGrn: alice.grn }]);
  });
});

describe("removeGroupMember", () => {
  it("deletes the one membership, addressed by the member's escaped GRN", async () => {
    const removed: string[] = [];
    server.use(
      http.delete(`${MEMBERS}/:member`, ({ request }) => {
        removed.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      removeGroupMember({ status: "idle" }, form(reportedAgent.grn)),
    ).resolves.toEqual({ status: "idle" });
    expect(removed).toEqual([
      `/orgs/acme/groups/eng/members/${encodeURIComponent(reportedAgent.grn)}`,
    ]);
  });

  it("renders a failed removal rather than throwing it at the boundary", async () => {
    let failing = true;
    server.use(
      http.delete(`${MEMBERS}/:member`, () =>
        failing
          ? HttpResponse.error()
          : new HttpResponse(null, { status: 204 }),
      ),
    );

    await expect(
      removeGroupMember({ status: "idle" }, form(alice.grn)),
    ).resolves.toEqual({
      status: "error",
      message: "the garam API could not be reached",
    });

    // The control: the same removal, answered, succeeds.
    failing = false;
    await expect(
      removeGroupMember({ status: "idle" }, form(alice.grn)),
    ).resolves.toEqual({ status: "idle" });
  });
});
