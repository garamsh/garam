import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentsPage from "@/app/console/orgs/[org]/agents/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/agents",
  useRouter: () => ({ push: () => {}, refresh: () => {} }),
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

// Radix Select measures with ResizeObserver.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

function renderAgents(query: Record<string, string> = {}) {
  return renderServerComponent(
    AgentsPage({
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

/** The table's rows' agent names, in order. */
function names(): string[] {
  const table = screen.getByRole("table", { name: "Agents" });
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

describe("agents", () => {
  it("lists the organization's agents alone, failing ones first (ADR-0116)", async () => {
    await renderAgents();

    // The organization also holds Alice; she is People's to list.
    expect(names()).toEqual(["1c3d5e7f90a2b4c6"]);
    expect(
      screen.getByRole("link", { name: "1c3d5e7f90a2b4c6" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/1c3d5e7f90a2b4c6");
    expect(screen.getByRole("columnheader", { name: /Runs on/ })).toBeVisible();
  });

  it("shows an agent by its name where it has one (ADR-0119)", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([{ ...reportedAgent, name: "Sherlock" }]),
      ),
    );
    await renderAgents();

    expect(names()).toEqual(["Sherlock"]);
  });

  it("gives each agent one state, saying what makes it so", async () => {
    await renderAgents();

    const failing = screen
      .getByRole("link", { name: "1c3d5e7f90a2b4c6" })
      .closest("tr") as HTMLElement;
    expect(failing).toHaveTextContent("Failing");
    expect(failing).toHaveTextContent("Provisioning failed; disconnected");
    expect(failing).toHaveTextContent("Nowhere yet");
  });

  it("filters by team from the team's members", async () => {
    // Engineering holds Alice and the reported agent.
    await renderAgents({ team: "eng" });

    expect(names()).toEqual(["1c3d5e7f90a2b4c6"]);
  });

  it("filters by what runs an agent, read from its assignee alone", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            // No legacy operatorGrn: the assignee says it is an operator.
            assignment: {
              assigneeGrn: "grn:acme:default:operator:field-one",
              epoch: 3,
              since: "2026-10-01T00:00:00Z",
            },
          },
        ]),
      ),
    );

    await renderAgents({ runs: "field-one" });

    expect(names()).toEqual(["1c3d5e7f90a2b4c6"]);
    expect(screen.getByRole("table")).toHaveTextContent("Field cluster one");
  });

  it("names a standalone host as one, from a supervisor assignee", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            assignment: {
              assigneeGrn: "grn:acme:default:supervisor:build-07",
              epoch: 1,
              since: "2026-10-01T00:00:00Z",
            },
          },
        ]),
      ),
    );

    await renderAgents({ runs: "standalone" });

    expect(names()).toEqual(["1c3d5e7f90a2b4c6"]);
    expect(screen.getByRole("table")).toHaveTextContent(
      "build-07 (standalone)",
    );
  });

  it("says a refused agents list is refused, never shown as empty", async () => {
    server.use(refuse("agents"));

    await renderAgents();

    expect(
      screen.getByText("You can't view this organization's agents"),
    ).toBeInTheDocument();
    expect(screen.queryByText("No agents yet.")).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("keeps creating on the page, inactive with its reason, where the server denies it", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "agent:create" ? "deny" : "allow",
      ),
    );

    await renderAgents();

    const create = screen.getByRole("button", { name: "New agent" });
    expect(create).toHaveAttribute("aria-disabled", "true");
    expect(create).toHaveAccessibleDescription(
      "No policy lets you create agents.",
    );
  });

  it("offers creating as a link where the server allows it", async () => {
    // The control for the case above.
    await renderAgents();

    expect(screen.getByRole("link", { name: "New agent" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/new",
    );
  });

  it("says no agents in one line, with the action to create one", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () => HttpResponse.json([])),
    );

    await renderAgents();

    expect(screen.getByText("No agents yet.")).toBeInTheDocument();
    // The header's and the empty state's: one action, in both places.
    expect(screen.getAllByRole("link", { name: "New agent" })).toHaveLength(2);
  });
});
