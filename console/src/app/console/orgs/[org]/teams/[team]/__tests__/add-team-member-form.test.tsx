import { render, screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { AddMemberForm } from "@/app/console/_components/membership/add-member-form";
import type { Candidate } from "@/features/orgs";
import { API_URL, alice, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-client/refusal-copy";

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

function renderForm(props: Partial<Parameters<typeof AddMemberForm>[0]>) {
  return render(
    <AddMemberForm
      org="acme"
      scope={{ kind: "team", id: "eng", name: "Engineering" }}
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

  it("posts the one chosen, says whom it added and goes back to the prompt", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm({});

    await choose(user, "field-agent");
    await user.click(screen.getByRole("button", { name: "Add to team" }));

    // The status line is always there, empty until the action answers,
    // so the wait is on what it says.
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent(
        "Added field-agent.",
      ),
    );
    expect(posted).toEqual([{ memberGrn: reportedAgent.grn }]);
    // The choice goes back to the prompt even while the member is still
    // offered, as it is here with no page to refresh the list.
    expect(
      screen.getByRole("combobox", { name: "Person or agent" }),
    ).toHaveTextContent("Choose who to add");
  });

  it("says a choice is needed when sent without one", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(MEMBERS, async ({ request }) => {
        posted.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm({});

    // The button is live before a choice, as it is before hydration.
    await user.click(screen.getByRole("button", { name: "Add to team" }));

    const choice = screen.getByRole("combobox", { name: "Person or agent" });
    await waitFor(() => expect(choice).toHaveAttribute("aria-invalid", "true"));
    expect(choice).toHaveAccessibleDescription(
      "Choose a person or an agent to add.",
    );
    expect(posted).toEqual([]);
    expect(screen.getByRole("status")).toHaveTextContent("");
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
    expect(screen.getByRole("status")).toHaveTextContent("");

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
