import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { AddTeamMemberForm } from "@/app/console/orgs/[org]/teams/[team]/_components/add-team-member-form";
import { API_URL, alice, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const MEMBERS = `${API_URL}/orgs/acme/groups/eng/members`;

const candidates = [
  { grn: alice.grn, kind: "person" as const, label: "Alice" },
  { grn: reportedAgent.grn, kind: "agent" as const, label: "field-agent" },
];

describe("add team member form", () => {
  it("posts the one chosen, picked by name and type", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(
      <AddTeamMemberForm
        org="acme"
        team="eng"
        candidates={candidates}
        unlisted={[]}
      />,
    );

    await user.selectOptions(
      screen.getByLabelText("Person or agent"),
      "field-agent · Agent",
    );
    await user.click(screen.getByRole("button", { name: "Add to team" }));

    await vi.waitFor(() =>
      expect(posted).toEqual([{ memberGrn: reportedAgent.grn }]),
    );
  });

  it("shows a failed addition rather than treating it as done", async () => {
    let refusing = true;
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        if (refusing) {
          return HttpResponse.json(
            { kind: "permission_denied", message: "permission denied" },
            { status: 403 },
          );
        }
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(
      <AddTeamMemberForm
        org="acme"
        team="eng"
        candidates={candidates}
        unlisted={[]}
      />,
    );

    await user.selectOptions(
      screen.getByLabelText("Person or agent"),
      "Alice · Person",
    );
    await user.click(screen.getByRole("button", { name: "Add to team" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );

    // The control: the same choice garam accepts is added and the
    // refusal clears.
    refusing = false;
    await user.selectOptions(
      screen.getByLabelText("Person or agent"),
      "Alice · Person",
    );
    await user.click(screen.getByRole("button", { name: "Add to team" }));
    await vi.waitFor(() => expect(posted).toEqual([{ memberGrn: alice.grn }]));
    expect(screen.queryByText("permission denied")).not.toBeInTheDocument();
  });

  it("says there is no one left to add instead of an empty choice", () => {
    render(
      <AddTeamMemberForm org="acme" team="eng" candidates={[]} unlisted={[]} />,
    );

    expect(
      screen.getByText(
        "Every person and agent of this organization is already in this team.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("says which lists were refused rather than claiming no one is left", () => {
    render(
      <AddTeamMemberForm
        org="acme"
        team="eng"
        candidates={[]}
        unlisted={["agents"]}
      />,
    );

    expect(
      screen.getByText(
        "This organization's agents are not listed to you, so none of them can be chosen here.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("No one you can list is outside this team."),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        "Every person and agent of this organization is already in this team.",
      ),
    ).not.toBeInTheDocument();
  });
});
