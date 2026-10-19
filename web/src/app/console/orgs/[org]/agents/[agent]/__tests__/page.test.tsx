import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentPage from "@/app/console/orgs/[org]/agents/[agent]/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { enter } from "@/testing/enter";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  fieldAgent,
  fieldOne,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

// The rename's Server Action stays real; the control service's
// authority is counted, which is what a re-read would mint again.
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
  usePathname: () => "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
}));

const ONE = `${API_URL}/orgs/acme/agent-definitions/9f2ac1b40d8e7a35`;

function renderAgent(agent = "9f2ac1b40d8e7a35") {
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
  it("names the agent, states its reports once in the header, and shows its definition", async () => {
    const { container } = await renderAgent();

    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
    // The identifier is offered to copy, in mono, and is not the title.
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "9f2ac1b40d8e7a35",
    );
    // The reports are the header's alone (ADR-0116): no second section
    // repeats them.
    expect(screen.getAllByText("Awaiting operator")).toHaveLength(1);
    expect(
      screen.queryByRole("heading", { name: "Reported" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Definition" }),
    ).toBeInTheDocument();
    expect(screen.getByText("haiku")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(
      screen.getByRole("link", { name: "Manage policies" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=agent%3A9f2ac1b40d8e7a35",
    );
    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
      "Overview",
      "Execution",
      "Settings",
    ]);
    expect(container.textContent).not.toContain("grn:");
  });

  it("calls an agent garam holds no name for unnamed, never by its identifier, and offers no rename", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions/1c3d5e7f90a2b4c6`, () =>
        HttpResponse.json(
          { kind: "not_found", message: "agent definition not found" },
          { status: 404 },
        ),
      ),
    );

    await renderAgent("1c3d5e7f90a2b4c6");

    expect(
      screen.getByRole("heading", { level: 1, name: "Unnamed agent" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "1c3d5e7f90a2b4c6" }),
    ).toBeNull();
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "1c3d5e7f90a2b4c6",
    );
    expect(screen.getByText("No definition in garam.")).toBeInTheDocument();
    // Nothing of it is garam's to rename.
    expect(screen.queryByRole("tab", { name: "Settings" })).toBeNull();
  });

  it("shows the states an operator and the agent reported once one claims it", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([{ ...reportedAgent, grn: fieldAgent.agentGrn }]),
      ),
    );

    await renderAgent();

    // In the header, each its own dot and label, said once.
    expect(screen.getAllByText("Provisioning failed")).toHaveLength(1);
    expect(screen.getAllByText("Disconnected")).toHaveLength(1);
    expect(screen.queryByText("Awaiting operator")).not.toBeInTheDocument();
  });

  it("renames the agent through the form under Settings", async () => {
    let body: unknown;
    server.use(
      http.patch(ONE, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ ...fieldAgent, name: "Ranger" });
      }),
    );
    const user = userEvent.setup();
    await renderAgent();
    await openTab("Settings");

    await user.clear(screen.getByLabelText("Name"));
    await enter(user, screen.getByLabelText("Name"), " Ranger ");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    // Inactive panels stay mounted, so the answer is read in the panel
    // that asked.
    expect(
      await within(
        screen.getByRole("tabpanel", { name: "Settings" }),
      ).findByRole("status"),
    ).toHaveTextContent("Renamed to Ranger.");
    expect(body).toEqual({ name: "Ranger" });
  });

  it("keeps an unsaved rename through a switch to another tab and back", async () => {
    const user = userEvent.setup();
    await renderAgent();
    await openTab("Settings");
    const name = screen.getByLabelText("Name");
    // The control: the field starts at the saved name, so what it holds
    // after the switch is what was typed.
    expect(name).toHaveValue("Scout");
    await user.clear(name);
    await enter(user, name, "Ranger");

    await openTab("Overview");
    await openTab("Settings");

    expect(screen.getByLabelText("Name")).toHaveValue("Ranger");
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

  it("leaves the rename out where the server denies it, and keeps it where the server cannot say", async () => {
    // The control: unanswered, the rename stays, and the server's
    // refusal of the write is the backstop (ADR-0103).
    server.use(answerPermissionChecks(() => "unknown"));
    const unknown = await renderAgent();
    expect(screen.getByRole("tab", { name: "Settings" })).toBeInTheDocument();
    unknown.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "agent:rename" ? "deny" : "allow",
      ),
    );
    await renderAgent();

    expect(screen.queryByRole("tab", { name: "Settings" })).toBeNull();
    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
  });

  it("shows no cut-over marker, even for a definition with one open (#1592)", async () => {
    server.use(
      http.get(ONE, () =>
        HttpResponse.json({ ...fieldAgent, cutover: { stage: "switched" } }),
      ),
    );

    await renderAgent();

    // The control: the definition itself rendered, values and all.
    expect(
      screen.getByRole("heading", { name: "Definition" }),
    ).toBeInTheDocument();
    expect(screen.getByText("haiku")).toBeInTheDocument();
    expect(screen.queryByText(/Managed by the Operator/)).toBeNull();
    expect(screen.queryByText(/Cut-over/)).toBeNull();
    expect(screen.queryByText(/archival/)).toBeNull();
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

  it("says a refused definition is refused rather than missing", async () => {
    let refusing = false;
    server.use(
      http.get(ONE, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "no" },
              { status: 403 },
            )
          : HttpResponse.json(fieldAgent),
      ),
    );
    // The control: answered, the same agent renders under its name and
    // says nothing is refused.
    const allowed = await renderAgent();
    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("You can't view this agent"),
    ).not.toBeInTheDocument();
    allowed.unmount();

    refusing = true;
    await renderAgent();

    expect(screen.getByText("You can't view this agent")).toBeInTheDocument();
    expect(screen.queryByRole("tab")).toBeNull();
  });

  describe("header", () => {
    const UNDEFINED = `${API_URL}/orgs/acme/agent-definitions/1c3d5e7f90a2b4c6`;

    /** The value a fact of the header holds. */
    function headerFact(term: string) {
      const dt = screen
        .getAllByText(term, { selector: "dt" })
        .find((element) => element.closest('[role="tabpanel"]') === null);
      return dt?.nextElementSibling as HTMLElement;
    }

    function heldBy(assignment: object | undefined) {
      server.use(
        http.get(UNDEFINED, () =>
          HttpResponse.json(
            { kind: "not_found", message: "no" },
            { status: 404 },
          ),
        ),
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([{ ...reportedAgent, assignment }]),
        ),
      );
    }

    it("states what holds an agent, read from its assignee, and why garam holds no definition", async () => {
      heldBy({
        assigneeGrn: "grn:acme:default:operator:field-one",
        epoch: 7,
        since: "2026-10-01T00:00:00Z",
      });

      await renderAgent("1c3d5e7f90a2b4c6");

      expect(headerFact("Runs on")).toHaveTextContent("Field cluster one");
      expect(
        screen.getByText("Created by Field cluster one's control service."),
      ).toBeInTheDocument();
    });

    it("shows the generation garam last activated where the read answers one (#1576)", async () => {
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([
            { ...reportedAgent, grn: fieldAgent.agentGrn, generation: "4" },
          ]),
        ),
      );

      await renderAgent();

      expect(headerFact("Generation")).toHaveTextContent("4");
    });

    it("omits the generation where the read answers none", async () => {
      // The control for the case above.
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([{ ...reportedAgent, grn: fieldAgent.agentGrn }]),
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

      await renderAgent("1c3d5e7f90a2b4c6");

      expect(headerFact("Runs on")).toHaveTextContent("build-07 (standalone)");
      expect(
        screen.getByText("Enrolled by the standalone host build-07."),
      ).toBeInTheDocument();
    });

    it("says what holds it is not checked where reports are refused, never nowhere", async () => {
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json(
            { kind: "permission_denied", message: "no" },
            { status: 403 },
          ),
        ),
      );

      await renderAgent();

      expect(headerFact("Runs on")).toHaveTextContent("Not checked");
      expect(headerFact("Last report")).toHaveTextContent("Not checked");
    });

    it("names who defined it", async () => {
      await renderAgent();

      expect(headerFact("Defined by")).toHaveTextContent("Alice");
    });
  });

  describe("Replace host", () => {
    function heldBy(assignment: object) {
      server.use(
        http.get(`${API_URL}/orgs/acme/agents`, () =>
          HttpResponse.json([
            { ...reportedAgent, grn: fieldAgent.agentGrn, assignment },
          ]),
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
        name: "Replace Scout's host?",
      });
      expect(dialog).toHaveTextContent(/build-07 stops running Scout/);
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
              agentGrn: fieldAgent.agentGrn,
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
