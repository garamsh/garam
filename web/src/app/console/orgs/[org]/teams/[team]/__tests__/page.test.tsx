import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import TeamPage from "@/app/console/orgs/[org]/teams/[team]/page";
import { slugFromGrn } from "@/features/orgs";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  alice,
  answerPermissionChecks,
  aliceMembership,
  fieldAgent,
  reportedAgent,
  signedOut,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// The removal action revalidates the page it came from.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/teams/eng",
}));

const MEMBERS = `${API_URL}/orgs/acme/groups/eng/members`;

function params(org: string, team: string) {
  return { params: Promise.resolve({ org, team }) };
}

/** What `/orgs/acme/groups/eng/members` answers with, for one test. */
function membersAre(...grns: string[]) {
  return http.get(MEMBERS, () =>
    HttpResponse.json(grns.map((memberGrn) => ({ memberGrn }))),
  );
}

function renderTeam() {
  return renderServerComponent(TeamPage(params("acme", "eng")));
}

/** Opens the choice of whom to add and answers what it offers. */
async function offered(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("combobox", { name: "Person or agent" }));
  const options = await screen.findAllByRole("option");
  return options.map((option) => option.textContent);
}

describe("team page", () => {
  it("shows the team's name and its members by name and type", async () => {
    const { container } = await renderTeam();

    expect(
      screen.getByRole("heading", { level: 1, name: "Engineering" }),
    ).toBeInTheDocument();
    // Presence: a row's accessible name is all of its cells.
    const person = screen.getByRole("row", { name: /Alice/ });
    expect(within(person).getByText("Person")).toBeInTheDocument();
    // Presence, for the same reason.
    const agent = screen.getByRole("row", {
      name: new RegExp(slugFromGrn(reportedAgent.grn)),
    });
    expect(within(agent).getByText("Agent")).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("links to the management of the team's policy attachments", async () => {
    await renderTeam();

    expect(
      screen.getByRole("link", { name: "Manage policies" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=team%3Aeng",
    );
  });

  it("offers only the people and agents the team does not hold", async () => {
    server.use(
      membersAre(alice.grn),
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json([
          aliceMembership,
          {
            userGrn: "grn:root:default:user:bob",
            nickname: "Bob",
            role: "member",
            since: "2026-01-02T03:04:05Z",
          },
        ]),
      ),
    );

    const user = userEvent.setup();
    await renderTeam();

    // A defined agent is offered by name before any operator claims
    // it, with its identifier and its state beside the name (ADR-0075).
    expect(await offered(user)).toEqual([
      "Bob bob",
      "Scout 9f2ac1b40d8e7a35 · awaiting operator",
      `Unnamed agent ${slugFromGrn(reportedAgent.grn)}`,
    ]);
  });

  it("tells two agents of one name apart by their rows and removes the one chosen", async () => {
    const first = fieldAgent.agentGrn;
    const second = "grn:acme:default:agent:0a1b2c3d4e5f6071";
    const removed: string[] = [];
    server.use(
      membersAre(first, second),
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([fieldAgent, { ...fieldAgent, agentGrn: second }]),
      ),
      http.delete(`${MEMBERS}/:member`, ({ params }) => {
        removed.push(decodeURIComponent(String(params.member)));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();

    await renderTeam();

    // Both are called Scout; each row carries its own identifier.
    const rows = screen.getAllByRole("row", { name: /Scout/ });
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByText("9f2ac1b40d8e7a35")).toBeInTheDocument();
    expect(within(rows[1]).getByText("0a1b2c3d4e5f6071")).toBeInTheDocument();

    // The sentences name the agent and never its identifier: the row
    // the control stands in is what tells the two apart, so the second
    // agent is the one removed and the first stays.
    await user.click(
      within(rows[1]).getByRole("button", {
        name: "Remove Scout from Engineering",
      }),
    );
    const dialog = screen.getByRole("alertdialog", {
      name: "Remove Scout from Engineering?",
    });
    expect(within(dialog).queryByText(/0a1b2c3d4e5f6071/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Remove from team" }));

    await vi.waitFor(() => expect(removed).toEqual([second]));
    // The removal answered closes the dialog, which is what hands the
    // page back.
    await vi.waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(
      within(screen.getByRole("row", { name: /9f2ac1b40d8e7a35/ })).getByRole(
        "button",
        { name: "Remove Scout from Engineering" },
      ),
    ).toBeInTheDocument();
  });

  it("names a defined agent member by the name its definition gives it", async () => {
    server.use(membersAre(fieldAgent.agentGrn));

    await renderTeam();

    // Presence: a row's accessible name is all of its cells.
    const row = screen.getByRole("row", { name: /Scout/ });
    expect(within(row).getByText("Agent")).toBeInTheDocument();
    expect(
      within(row).queryByText("Not found in this organization"),
    ).not.toBeInTheDocument();
  });

  it("says agents may be missing, not that none can be chosen, when one agent list is refused", async () => {
    // The control: with both agent lists read, nothing is said missing.
    server.use(membersAre(alice.grn));
    const user = userEvent.setup();
    const allowed = await renderTeam();
    expect(screen.queryByText(/may be missing here/)).not.toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderTeam();

    expect(
      screen.getByText(
        "This organization's agent definitions are not listed to you, so some agents may be missing here.",
      ),
    ).toBeInTheDocument();
    // The reported list still offers its agent, so "none can be chosen"
    // would be false.
    expect(await offered(user)).toContain(
      `Unnamed agent ${slugFromGrn(reportedAgent.grn)}`,
    );
    expect(
      screen.queryByText(/none of them can be chosen/),
    ).not.toBeInTheDocument();
  });

  it("says no agent can be chosen when both agent lists are refused", async () => {
    // The control: with both agent lists read, no agent list is said
    // refused.
    server.use(membersAre(alice.grn));
    const allowed = await renderTeam();
    expect(
      screen.queryByText(/agents are not listed to you/),
    ).not.toBeInTheDocument();
    allowed.unmount();

    server.use(
      ...["agent-definitions", "agents"].map((segment) =>
        http.get(`${API_URL}/orgs/acme/${segment}`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
        ),
      ),
    );

    await renderTeam();

    expect(
      screen.getByText(
        "This organization's agents are not listed to you, so none of them can be chosen here.",
      ),
    ).toBeInTheDocument();
  });

  it("keeps a member the organization no longer holds visible and removable", async () => {
    server.use(
      membersAre(
        alice.grn,
        "grn:root:default:user:gone",
        "grn:acme:default:agent:retired",
        "grn:acme:default:project:frontend",
      ),
    );

    await renderTeam();

    for (const identifier of ["gone", "retired", "frontend"]) {
      // Presence: the row names the identifier among its other cells.
      const row = screen.getByRole("row", { name: new RegExp(identifier) });
      expect(
        within(row).getByText("Not found in this organization"),
      ).toBeInTheDocument();
      // Presence: the button's name goes on to name the member and
      // the team, and the row already fixes which member.
      expect(
        within(row).getByRole("button", { name: /^Remove / }),
      ).toBeInTheDocument();
    }
    // A person nobody resolves is not given a name-shaped placeholder.
    // Rows are found by presence, as above.
    const gone = screen.getByRole("row", { name: /gone/ });
    expect(within(gone).getByText("Unknown person")).toBeInTheDocument();
    // A GRN no known type claims is not shown as a person or an agent.
    const unknown = screen.getByRole("row", { name: /frontend/ });
    expect(within(unknown).getByText("Unknown")).toBeInTheDocument();
    expect(within(unknown).getByText("Unknown identity")).toBeInTheDocument();
    // The control: a member the organization does hold carries no such
    // mark, so the mark is the lookup failing and not every row.
    const held = screen.getByRole("row", { name: /Alice/ });
    expect(
      within(held).queryByText("Not found in this organization"),
    ).not.toBeInTheDocument();
  });

  // A caller may read the team and still be refused a list its members
  // are looked up in. The team is not missing, and a member the list
  // was not read for is not missing either.
  it.each([
    ["agents", "Agent", "agents"],
    ["members", "Person", "people"],
  ])(
    "keeps the team readable when the %s list is refused",
    async (segment, type, listed) => {
      // The control: with the list answered, no member is unchecked, so
      // the mark below is the refusal and not every render.
      const allowed = await renderTeam();
      expect(screen.queryByText("Not checked")).not.toBeInTheDocument();
      allowed.unmount();

      server.use(
        http.get(`${API_URL}/orgs/acme/${segment}`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
        ),
      );

      await renderTeam();

      expect(
        screen.getByRole("heading", { level: 1, name: "Engineering" }),
      ).toBeInTheDocument();
      const refused = screen
        .getAllByRole("row")
        .find((row) => within(row).queryByText(type) !== null);
      expect(refused).toBeDefined();
      if (refused === undefined) return;
      expect(within(refused).getByText("Not checked")).toBeInTheDocument();
      expect(
        within(refused).getByText(
          `This organization's ${listed} are not listed to you`,
        ),
      ).toBeInTheDocument();
      expect(
        within(refused).queryByText("Not found in this organization"),
      ).not.toBeInTheDocument();
      // Presence: the button's name goes on to name the member.
      expect(
        within(refused).getByRole("button", { name: /^Remove / }),
      ).toBeInTheDocument();
    },
  );

  it("says the team has no members rather than showing an empty table", async () => {
    server.use(membersAre());

    await renderTeam();

    expect(screen.getByText("This team has no members.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("says the team is refused rather than missing when garam answers 403", async () => {
    // The control: the same team, answered, renders under its name.
    const allowed = await renderTeam();
    expect(
      screen.getByRole("heading", { level: 1, name: "Engineering" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/groups/eng`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderTeam();

    expect(screen.getByRole("status")).toHaveTextContent(
      // Presence: the notice goes on to say where access comes from.
      /^You cannot view this team/,
    );
    expect(screen.queryByText("Engineering")).not.toBeInTheDocument();
  });

  it("renders not-found for a team that does not exist", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups/eng`, () =>
        HttpResponse.json(
          { kind: "not_found", message: "no" },
          { status: 404 },
        ),
      ),
    );

    await expect(renderTeam()).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("renders not-found when the organization is not the caller's", async () => {
    // The control: the same team, answered, renders under its name.
    const allowed = await renderTeam();
    expect(
      screen.getByRole("heading", { level: 1, name: "Engineering" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
      http.get(`${API_URL}/orgs/acme/*`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await expect(renderTeam()).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than showing an empty team", async () => {
    server.use(http.get(MEMBERS, () => HttpResponse.error()));

    await renderTeam();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This team has no members."),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page", async () => {
    // The control: the same team, answered, renders under its name.
    const allowed = await renderTeam();
    expect(
      screen.getByRole("heading", { level: 1, name: "Engineering" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(...signedOut());

    await expect(renderTeam()).rejects.toThrow("NEXT_REDIRECT:/login");
  });
  it("keeps the rename in Settings and the delete in its danger zone", async () => {
    const user = userEvent.setup();
    await renderTeam();

    expect(screen.queryByLabelText("Team name")).not.toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Settings" }));

    expect(screen.getByLabelText("Team name")).toHaveValue("Engineering");
    const danger = screen.getByRole("region", { name: "Danger zone" });
    expect(
      within(danger).getByRole("button", { name: "Delete team" }),
    ).toBeInTheDocument();
  });

  it("leaves out each write the server denies and keeps the rest", async () => {
    // The control: every write allowed, every control is there.
    const user = userEvent.setup();
    const allowed = await renderTeam();
    expect(
      screen.getByRole("combobox", { name: "Person or agent" }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /^Remove / }).length,
    ).toBeGreaterThan(0);
    allowed.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "group:member-add" || action === "group:member-remove"
          ? "deny"
          : "allow",
      ),
    );
    await renderTeam();

    expect(
      screen.getByRole("heading", { level: 1, name: "Engineering" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Remove / }),
    ).not.toBeInTheDocument();
    // The members are still listed; only the control went.
    // Presence: a row's accessible name is all of its cells.
    expect(screen.getByRole("row", { name: /Alice/ })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Settings" }));
    expect(screen.getByLabelText("Team name")).toBeInTheDocument();
  });

  it("drops the Settings tab when the server denies renaming and deleting", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "group:update" || action === "group:delete"
          ? "deny"
          : "allow",
      ),
    );

    await renderTeam();

    expect(screen.getByRole("tab", { name: "Overview" })).toBeInTheDocument();
    expect(
      screen.queryByRole("tab", { name: "Settings" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Danger zone" }),
    ).not.toBeInTheDocument();
  });

  it("shows every control when the server cannot say", async () => {
    server.use(answerPermissionChecks(() => "unknown"));
    const user = userEvent.setup();

    await renderTeam();

    expect(
      screen.getByRole("combobox", { name: "Person or agent" }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /^Remove / }).length,
    ).toBeGreaterThan(0);
    await user.click(screen.getByRole("tab", { name: "Settings" }));
    expect(screen.getByLabelText("Team name")).toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Danger zone" }),
    ).toBeInTheDocument();
  });

  it("asks the server once, about this team, for every write it offers", async () => {
    const asked: unknown[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/permission-checks`,
        async ({ request }) => {
          const body = (await request.json()) as {
            checks: { action: string }[];
          };
          asked.push(body);
          return HttpResponse.json({
            results: body.checks.map((check) => ({
              ...check,
              decision: "allow",
            })),
          });
        },
      ),
    );

    await renderTeam();

    const group = { type: "group", id: "eng" };
    expect(asked).toEqual([
      {
        checks: [
          { action: "group:member-add", resource: group },
          { action: "group:member-remove", resource: group },
          { action: "group:update", resource: group },
          { action: "group:delete", resource: group },
        ],
      },
    ]);
  });
  it("hides the rename alone when only renaming is denied", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "group:update" ? "deny" : "allow",
      ),
    );
    const user = userEvent.setup();
    await renderTeam();

    await user.click(screen.getByRole("tab", { name: "Settings" }));

    expect(screen.queryByLabelText("Team name")).not.toBeInTheDocument();
    // The control: the delete, answered apart, stays.
    expect(
      screen.getByRole("region", { name: "Danger zone" }),
    ).toBeInTheDocument();
  });

  it("hides the delete alone when only deleting is denied", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "group:delete" ? "deny" : "allow",
      ),
    );
    const user = userEvent.setup();
    await renderTeam();

    await user.click(screen.getByRole("tab", { name: "Settings" }));

    expect(
      screen.queryByRole("region", { name: "Danger zone" }),
    ).not.toBeInTheDocument();
    // The control: the rename, answered apart, stays.
    expect(screen.getByLabelText("Team name")).toBeInTheDocument();
  });
});
