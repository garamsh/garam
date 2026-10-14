import { render, screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { AddTeamMemberForm } from "@/app/console/orgs/[org]/teams/[team]/_components/add-team-member-form";
import type { Candidate } from "@/app/console/orgs/[org]/teams/[team]/_components/team-member";
import { API_URL, alice, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const MEMBERS = `${API_URL}/orgs/acme/groups/eng/members`;

const candidates: Candidate[] = [
  { grn: alice.grn, kind: "person", name: "Alice", identifier: "alice" },
  {
    grn: reportedAgent.grn,
    kind: "agent",
    name: "field-agent",
    identifier: "fa01",
  },
];

/**
 * Opens the choice and picks the option whose name starts with `name`.
 * Presence: an option's name goes on to carry the identifier.
 */
async function choose(user: UserEvent, name: string) {
  await user.click(screen.getByRole("combobox", { name: "Person or agent" }));
  await user.click(
    await screen.findByRole("option", { name: new RegExp(`^${name} `) }),
  );
}

function renderForm(props: Partial<Parameters<typeof AddTeamMemberForm>[0]>) {
  return render(
    <AddTeamMemberForm
      org="acme"
      team="eng"
      candidates={candidates}
      unlisted={[]}
      {...props}
    />,
  );
}

describe("add team member form", () => {
  it("offers people and agents under their type, each with its identifier", async () => {
    const user = userEvent.setup();
    renderForm({});

    await user.click(screen.getByRole("combobox", { name: "Person or agent" }));

    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual(["Alice alice", "field-agent fa01"]);
    expect(screen.getByRole("group", { name: "People" })).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Agents" })).toBeInTheDocument();
  });

  it("posts the one chosen and offers nothing to post before a choice", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm({});

    expect(screen.getByRole("button", { name: "Add to team" })).toBeDisabled();
    await choose(user, "field-agent");
    await user.click(screen.getByRole("button", { name: "Add to team" }));

    await vi.waitFor(() =>
      expect(posted).toEqual([{ memberGrn: reportedAgent.grn }]),
    );
  });

  it("attaches a refusal to the choice and sends the same choice again", async () => {
    let refusing = true;
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm({});

    // Not the first offered, so a reset to it would show as well as a
    // reset to the prompt.
    await choose(user, "field-agent");
    await user.click(screen.getByRole("button", { name: "Add to team" }));

    const choice = screen.getByRole("combobox", { name: "Person or agent" });
    await waitFor(() => expect(choice).toHaveAttribute("aria-invalid", "true"));
    expect(choice).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied);
    // Presence: the trigger shows the option's name and identifier.
    expect(choice).toHaveTextContent(/^field-agent/);

    // The control: submitted again untouched, garam accepts the same
    // choice and the error clears.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Add to team" }));
    await waitFor(() => expect(choice).not.toHaveAttribute("aria-invalid"));
    expect(posted).toEqual([
      { memberGrn: reportedAgent.grn },
      { memberGrn: reportedAgent.grn },
    ]);
  });

  it("says there is no one left to add instead of an empty choice", () => {
    renderForm({ candidates: [] });

    expect(
      screen.getByText(
        "Every person and agent of this organization is already in this team.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("says which lists were refused rather than claiming no one is left", () => {
    renderForm({ candidates: [], unlisted: ["agents"] });

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
