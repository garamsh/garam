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
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
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

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Renamed to Ranger.",
    );
    expect(body).toEqual({ name: "Ranger" });
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
      screen.queryByText("This agent is not shown to you"),
    ).not.toBeInTheDocument();
    allowed.unmount();

    refusing = true;
    await renderAgent();

    expect(
      screen.getByText("This agent is not shown to you"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tab")).toBeNull();
  });
});
