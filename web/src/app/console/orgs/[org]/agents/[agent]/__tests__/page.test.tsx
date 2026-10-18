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
  it("names the agent, keeps what was requested apart from what was reported, and awaits an operator", async () => {
    const { container } = await renderAgent();

    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
    // The identifier is offered to copy, in mono, and is not the title.
    expect(
      screen.getByRole("group", { name: "Agent identifier" }),
    ).toHaveTextContent("9f2ac1b40d8e7a35");
    expect(
      screen.getByRole("heading", { name: "Reported" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Awaiting operator").length).toBeGreaterThan(0);
    expect(
      screen.getByRole("heading", { name: "Requested" }),
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
    expect(
      screen.getByRole("group", { name: "Agent identifier" }),
    ).toHaveTextContent("1c3d5e7f90a2b4c6");
    expect(screen.getByText("No definition in garam")).toBeInTheDocument();
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

    // In the header and under Reported, each its own dot and label.
    expect(screen.getAllByText("Provisioning failed")).toHaveLength(2);
    expect(screen.getAllByText("Disconnected")).toHaveLength(2);
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

  it.each([
    ["switched", "Managed by the Operator"],
    ["frozen", "Cut-over in progress"],
  ] as const)(
    "tracks a %s definition's cutover read-only, says its values are archival, and keeps the rename",
    async (stage, state) => {
      server.use(
        http.get(ONE, () =>
          HttpResponse.json({ ...fieldAgent, cutover: { stage } }),
        ),
      );

      await renderAgent();

      const track = screen.getByRole("list", { name: "Cut-over" });
      expect(
        within(track)
          .getAllByRole("listitem")
          .find((item) => item.getAttribute("aria-current") === "step"),
      ).toHaveTextContent(state);
      // The track reports; it offers no transition.
      expect(within(track).queryByRole("button")).toBeNull();
      expect(
        screen.getByText("These settings are archival and read-only in garam"),
      ).toBeInTheDocument();
      // The archived values are still shown, as they were frozen, and
      // the sentence a legacy definition carries is gone.
      expect(screen.getByText("haiku")).toBeInTheDocument();
      expect(
        screen.queryByText("Saved values; application is not reported."),
      ).not.toBeInTheDocument();
      // Renaming still reaches garam (ADR-0086).
      await openTab("Settings");
      expect(screen.getByLabelText("Name")).toHaveValue("Scout");
    },
  );

  it("says nothing of a cutover for a definition with none open", async () => {
    // A rolled-back attempt is read as none, so this is its state too.
    await renderAgent();

    expect(
      screen.getByText("Saved values; application is not reported."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "Cut-over" })).toBeNull();
    // Presence, not identity: no sentence on the page mentions an
    // archive.
    expect(screen.queryByText(/archival/)).not.toBeInTheDocument();
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

  it("says when a report last landed is not checked where reports are refused, never none", async () => {
    // The control: answered and unclaimed, no report has landed.
    const allowed = await renderAgent();
    expect(
      screen.getByText("Last report").nextElementSibling,
    ).toHaveTextContent("None");
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );
    await renderAgent();

    expect(
      screen.getByText("Last report").nextElementSibling,
    ).toHaveTextContent("Not checked");
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
});
