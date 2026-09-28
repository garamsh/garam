import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import TeamPage from "@/app/console/orgs/[org]/teams/[team]/page";
import { slugFromGrn } from "@/features/orgs";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  alice,
  aliceMembership,
  fieldAgent,
  reportedAgent,
  signedOut,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

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

    await renderTeam();

    const choice = screen.getByLabelText("Person or agent");
    const offered = within(choice)
      .getAllByRole("option")
      .map((option) => option.textContent);
    // A defined agent is offered by name before any operator claims
    // it, with its identifier and its state beside the name (ADR-0075).
    expect(offered).toEqual([
      "Choose who to add",
      "Bob · Person",
      "Scout (9f2ac1b40d8e7a35) · awaiting operator · Agent",
      `${slugFromGrn(reportedAgent.grn)} · Agent`,
    ]);
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
    const offered = within(screen.getByLabelText("Person or agent"))
      .getAllByRole("option")
      .map((option) => option.textContent);
    expect(offered).toContain(`${slugFromGrn(reportedAgent.grn)} · Agent`);
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
    ["agents", "Agent", "agents are"],
    ["members", "Person", "people are"],
  ])(
    "keeps the team readable when the %s list is refused",
    async (segment, type, marker) => {
      // The control: with the list answered, no member is unchecked, so
      // the mark below is the refusal and not every render.
      // Presence: the mark is a sentence that goes on to name the list.
      const allowed = await renderTeam();
      expect(screen.queryByText(/^Not checked:/)).not.toBeInTheDocument();
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
      // Presence: the mark continues with the list it could not read.
      expect(
        within(refused).getByText(new RegExp(`^Not checked: .*${marker}`)),
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

    expect(
      screen.getByRole("heading", { name: "You cannot view this team" }),
    ).toBeInTheDocument();
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
});
