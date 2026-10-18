import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import MembersPage from "@/app/console/orgs/[org]/members/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  fieldAgent,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const route = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/members",
  useRouter: () => ({ push: route.push, refresh: () => {} }),
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

// cmdk-free, but Radix Select measures with ResizeObserver.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

function renderMembers(query: Record<string, string> = {}) {
  return renderServerComponent(
    MembersPage({
      params: Promise.resolve({ org: "acme" }),
      searchParams: Promise.resolve(query),
    }),
    { wrapper: TooltipProvider },
  );
}

function refuse(path: string) {
  return http.get(`${API_URL}/orgs/acme/${path}`, () =>
    HttpResponse.json(
      { kind: "permission_denied", message: "no" },
      { status: 403 },
    ),
  );
}

/** The table's rows' member names, in order. */
function names(): string[] {
  const table = screen.getByRole("table", { name: "People and agents" });
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map(
      (row) =>
        within(row)
          .getAllByRole("link")
          .find((link) => link.getAttribute("href")?.startsWith("/console"))
          ?.textContent ?? "",
    );
}

describe("people and agents", () => {
  it("lists people and agents together, each linked to its own page", async () => {
    await renderMembers();

    // The failing reported agent leads the agents.
    expect(names()).toEqual(["Alice", "Unnamed agent", "Scout"]);
    expect(screen.getByRole("link", { name: "Alice" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/members/alice",
    );
    expect(screen.getByRole("link", { name: "Scout" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
    );
  });

  it("gives each agent one state, saying what makes it so", async () => {
    await renderMembers();

    const failing = screen
      .getByRole("link", { name: "Unnamed agent" })
      .closest("tr") as HTMLElement;
    expect(failing).toHaveTextContent("Failing");
    expect(failing).toHaveTextContent("Provisioning failed; disconnected");
    const unplaced = screen
      .getByRole("link", { name: "Scout" })
      .closest("tr") as HTMLElement;
    expect(unplaced).toHaveTextContent("Not placed");
    expect(unplaced).toHaveTextContent("Nowhere yet");
  });

  it("marks a definition that is cutting over", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([{ ...fieldAgent, cutover: { stage: "frozen" } }]),
      ),
    );

    await renderMembers();

    const scout = screen
      .getByRole("link", { name: "Scout" })
      .closest("tr") as HTMLElement;
    expect(scout).toHaveTextContent("Cut-over in progress");
    // Control: an agent no cutover touches carries no marker.
    expect(
      screen.getByRole("link", { name: "Unnamed agent" }).closest("tr"),
    ).not.toHaveTextContent("Cut-over");
  });

  it("marks a definition past frozen as cut over, beside one still cutting over (#1171)", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([
          { ...fieldAgent, cutover: { stage: "frozen" } },
          {
            ...fieldAgent,
            agentGrn: "grn:acme:default:agent:7e6d5c4b3a291807",
            name: "Moved",
            cutover: { stage: "switched" },
          },
        ]),
      ),
    );

    await renderMembers();

    expect(
      screen.getByRole("link", { name: "Moved" }).closest("tr"),
    ).toHaveTextContent("Managed by the Operator");
    expect(
      screen.getByRole("link", { name: "Scout" }).closest("tr"),
    ).toHaveTextContent("Cut-over in progress");
  });

  it("filters by kind", async () => {
    await renderMembers({ kind: "agent" });

    expect(names()).toEqual(["Unnamed agent", "Scout"]);
    expect(screen.getByRole("button", { name: /^Agents/ })).toHaveAttribute(
      "aria-current",
      "true",
    );
  });

  it("filters by team from the team's members", async () => {
    // Engineering holds Alice and the reported agent.
    await renderMembers({ team: "eng" });

    expect(names()).toEqual(["Alice", "Unnamed agent"]);
  });

  it("filters by what an agent runs on", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            assignment: {
              assigneeGrn: "grn:acme:default:operator:field-one",
              operatorGrn: "grn:acme:default:operator:field-one",
              epoch: 3,
              since: "2026-10-01T00:00:00Z",
            },
          },
        ]),
      ),
    );

    await renderMembers({ runs: "field-one" });

    expect(names()).toEqual(["Unnamed agent"]);
    expect(screen.getByRole("table")).toHaveTextContent(
      "Field cluster one, operator, epoch 3",
    );
  });

  it("says a refused list of people, and keeps the agents", async () => {
    server.use(refuse("members"));

    await renderMembers();

    expect(
      screen.getByText("You can't view this organization's people"),
    ).toBeInTheDocument();
    expect(names()).toEqual(["Unnamed agent", "Scout"]);
  });

  it("keeps inviting on the page, inactive with its reason, where the server denies it", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "org:invite" ? "deny" : "allow",
      ),
    );

    await renderMembers();

    const invite = screen.getByRole("button", { name: "Invite people" });
    expect(invite).toHaveAttribute("aria-disabled", "true");
    expect(invite).toHaveAccessibleDescription(
      "Inviting people needs org:invite, and no policy grants it to you.",
    );
    // Control: creating an agent, which the server allows, is a link.
    expect(screen.getByRole("link", { name: "New agent" })).toBeInTheDocument();
  });

  it("adds the chosen members to a team, one request each", async () => {
    const added: string[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/groups/:group/members`,
        async ({ request, params }) => {
          const { memberGrn } = (await request.json()) as {
            memberGrn: string;
          };
          added.push(`${String(params.group)} ${memberGrn}`);
          return HttpResponse.json({ memberGrn }, { status: 201 });
        },
      ),
    );
    const user = userEvent.setup();
    await renderMembers();

    await user.click(screen.getByRole("checkbox", { name: "Select Alice" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Scout" }));
    expect(screen.getByText("2 selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add to team" }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Add Alice and Scout to a team?",
    });
    await user.click(within(dialog).getByRole("combobox", { name: "Team" }));
    await user.click(screen.getByRole("option", { name: "Engineering" }));
    await user.click(
      within(dialog).getByRole("button", { name: "Add to team" }),
    );

    expect(await screen.findByText("Added 2 members")).toBeInTheDocument();
    expect(added).toEqual([
      "eng grn:root:default:user:alice",
      `eng ${fieldAgent.agentGrn}`,
    ]);
  });

  it("offers no team the server denies adding to", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "group:member-add" ? "deny" : "allow",
      ),
    );

    await renderMembers();

    // Engineering is the only team, so adding is kept, inactive.
    expect(screen.getByRole("button", { name: "Add to team" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });
});
