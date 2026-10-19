import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentPage from "@/app/console/orgs/[org]/agents/[agent]/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import { API_URL, fieldOne, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

// The control service's authority is counted, which is what a re-read
// would mint again.
const minted = vi.hoisted(() => ({ count: 0 }));
vi.mock("@/features/orgs/actions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/orgs/actions")>()),
  mintOperationAuthority: async () => {
    minted.count += 1;
    return { status: "ok", authority: "test-authority" };
  },
}));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/agents/1c3d5e7f90a2b4c6",
}));

function renderAgent(agent = "1c3d5e7f90a2b4c6") {
  return renderServerComponent(
    AgentPage({ params: Promise.resolve({ org: "acme", agent }) }),
    { wrapper: TooltipProvider },
  );
}

/** Opens one of the page's tabs, as a visitor does. */
async function openTab(name: string) {
  await userEvent.setup().click(screen.getByRole("tab", { name }));
}

describe("agent page", () => {
  it("calls the agent unnamed, never by its identifier, and states its reports once in the header", async () => {
    const { container } = await renderAgent();

    // garam holds no name for an agent (ADR-0118).
    expect(
      screen.getByRole("heading", { level: 1, name: "Unnamed agent" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "1c3d5e7f90a2b4c6" }),
    ).toBeNull();
    // The identifier is offered to copy, in mono, and is not the title.
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "1c3d5e7f90a2b4c6",
    );
    // The reports are the header's alone (ADR-0116): no second section
    // repeats them.
    expect(screen.getAllByText("Provisioning failed")).toHaveLength(1);
    expect(screen.getAllByText("Disconnected")).toHaveLength(1);
    expect(
      screen.queryByRole("heading", { name: "Reported" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Nothing holds it yet.")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Manage policies" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=agent%3A1c3d5e7f90a2b4c6",
    );
    // Nothing of it is garam's to rename.
    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
      "Overview",
      "Execution",
    ]);
    expect(container.textContent).not.toContain("grn:");
  });

  it("answers 404 for an identifier the agents list does not hold", async () => {
    await expect(renderAgent("9f2ac1b40d8e7a35")).rejects.toThrow(
      "NEXT_NOT_FOUND",
    );
  });

  it("says a refused agents list is refused rather than missing", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderAgent();

    expect(screen.getByText("You can't view this agent")).toBeInTheDocument();
    expect(screen.queryByRole("tab")).toBeNull();
  });

  it("reads the execution once, and a switch back to Execution does not read it again", async () => {
    const endpoint = "https://control.example.test/garam";
    const hosted = "grn:acme:default:operator:hosted";
    let reads = 0;
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          {
            ...fieldOne,
            grn: hosted,
            name: "Hosted",
            controlEndpoint: endpoint,
          },
        ]),
      ),
      http.get(`${API_URL}/orgs/acme/operators/hosted/delegation`, () =>
        HttpResponse.json({
          grantId: "grant-1",
          operatorGrn: hosted,
          controllers: [fieldOne.grn],
          operations: ["agent:execution-read"],
          expiresAt: "2099-01-01T00:00:00Z",
          grantedBy: "grn:root:default:user:alice",
          grantedAt: "2026-10-01T00:00:00Z",
        }),
      ),
      http.get(`${endpoint}/v1/orgs/acme/agents/:agent/execution`, () => {
        reads += 1;
        return HttpResponse.json({
          desired: { revision: "1", pending: true },
          rendered: null,
          effective: null,
        });
      }),
    );
    minted.count = 0;
    await renderAgent();
    await openTab("Execution");
    expect(await screen.findByText("Revision 1 (pending)")).toBeInTheDocument();

    await openTab("Overview");
    await openTab("Execution");

    expect(screen.getByText("Revision 1 (pending)")).toBeInTheDocument();
    expect(reads).toBe(1);
    expect(minted.count).toBe(1);
  });

  it("says a missing control service under Execution and links to where it is fixed", async () => {
    await renderAgent();
    await openTab("Execution");

    expect(
      screen.getByText(/No control service is registered/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open operators" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators");
  });

  it("links to no page the caller was refused: an unlisted operators list gets the reason alone", async () => {
    // The control: with the list read, the same section links to it.
    const listed = await renderAgent();
    await openTab("Execution");
    expect(
      screen.getByRole("link", { name: "Open operators" }),
    ).toBeInTheDocument();
    listed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );
    await renderAgent();
    await openTab("Execution");

    expect(
      screen.getByText(/garam refused to list this organization's operators/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /^Open/ })).toBeNull();
  });

  describe("header", () => {
    /** The value a fact of the header holds. */
    function headerFact(term: string) {
      const dt = screen
        .getAllByText(term, { selector: "dt" })
        .find((element) => element.closest('[role="tabpanel"]') === null);
      return dt?.nextElementSibling as HTMLElement;
    }

    function heldBy(assignment: object | undefined) {
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([{ ...reportedAgent, assignment }]),
        ),
      );
    }

    it("states what holds an agent, read from its assignee, and where it came from", async () => {
      heldBy({
        assigneeGrn: "grn:acme:default:operator:field-one",
        epoch: 7,
        since: "2026-10-01T00:00:00Z",
      });

      await renderAgent();

      expect(headerFact("Runs on")).toHaveTextContent("Field cluster one");
      expect(
        screen.getByText("Created by Field cluster one's control service."),
      ).toBeInTheDocument();
    });

    it("shows the generation garam last activated where the read answers one (#1576)", async () => {
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([{ ...reportedAgent, generation: "4" }]),
        ),
      );

      await renderAgent();

      expect(headerFact("Generation")).toHaveTextContent("4");
    });

    it("omits the generation where the read answers none", async () => {
      // The control for the case above.
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([reportedAgent]),
        ),
      );

      await renderAgent();

      expect(headerFact("Last report")).toBeDefined();
      expect(
        screen.queryByText("Generation", { selector: "dt" }),
      ).not.toBeInTheDocument();
    });

    it("says a standalone host enrolled an agent its supervisor holds", async () => {
      heldBy({
        assigneeGrn: "grn:acme:default:supervisor:build-07",
        epoch: 2,
        since: "2026-10-01T00:00:00Z",
      });

      await renderAgent();

      expect(headerFact("Runs on")).toHaveTextContent("build-07 (standalone)");
      expect(
        screen.getByText("Enrolled by the standalone host build-07."),
      ).toBeInTheDocument();
    });
  });

  describe("Replace host", () => {
    function heldBy(assignment: object) {
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([{ ...reportedAgent, assignment }]),
        ),
      );
    }

    it("approves a new standalone host from exactly the assignment the header read", async () => {
      heldBy({
        assigneeGrn: "grn:acme:default:supervisor:build-07",
        epoch: 2,
        since: "2026-10-01T00:00:00Z",
      });
      const sent: unknown[] = [];
      server.use(
        http.post(
          `${API_URL}/orgs/acme/agent-enrollments`,
          async ({ request }) => {
            sent.push(await request.json());
            return HttpResponse.json({}, { status: 201 });
          },
        ),
      );
      const user = userEvent.setup();
      await renderAgent();

      await user.click(screen.getByRole("button", { name: "Replace host" }));
      const dialog = screen.getByRole("alertdialog", {
        name: "Replace Unnamed agent's host?",
      });
      expect(dialog).toHaveTextContent(/build-07 stops running Unnamed agent/);
      await user.click(
        within(dialog).getByRole("textbox", {
          name: "What garam standalone prepare printed",
        }),
      );
      await user.paste(
        JSON.stringify({ requestId: "r-1", secretSha256: "b".repeat(64) }),
      );
      await user.type(
        within(dialog).getByRole("textbox", { name: "Host name" }),
        "build-09",
      );
      await user.click(
        within(dialog).getByRole("button", { name: "Approve replacement" }),
      );

      await vi.waitFor(() =>
        expect(sent).toEqual([
          expect.objectContaining({
            replaces: {
              agentGrn: reportedAgent.grn,
              assigneeGrn: "grn:acme:default:supervisor:build-07",
              epoch: 2,
            },
          }),
        ]),
      );
    });

    it("stays inactive for an agent an operator holds, since its control service replaces it", async () => {
      heldBy({
        assigneeGrn: "grn:acme:default:operator:field-one",
        epoch: 3,
        since: "2026-10-01T00:00:00Z",
      });

      await renderAgent();

      const replace = screen.getByRole("button", { name: "Replace host" });
      expect(replace).toHaveAttribute("aria-disabled", "true");
      expect(replace).toHaveAccessibleDescription(
        "Field cluster one's control service replaces this host.",
      );
    });
  });
});
