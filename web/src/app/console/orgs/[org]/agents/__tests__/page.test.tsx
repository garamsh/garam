import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentsPage from "@/app/console/orgs/[org]/agents/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
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
}));

function params(org: string) {
  return { params: Promise.resolve({ org }) };
}

/** Renders the page under the tooltip provider the console mounts. */
function renderPage() {
  return renderServerComponent(AgentsPage(params("acme")), {
    wrapper: TooltipProvider,
  });
}

function refuse(path: string) {
  return http.get(`${API_URL}/orgs/acme/${path}`, () =>
    HttpResponse.json(
      { kind: "permission_denied", message: "no" },
      { status: 403 },
    ),
  );
}

/** The table row an agent is listed in, found by the text it carries. */
function rowOf(text: string): HTMLElement {
  const row = screen.getByText(text).closest("tr");
  if (row === null) throw new Error(`no row holds ${text}`);
  return row;
}

/** The names in the table's rows, top to bottom. */
function listedNames(): string[] {
  const table = screen.getByRole("table", { name: "Agents" });
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("link")[0]?.textContent ?? "");
}

describe("agents section", () => {
  it("lists a defined agent by name before any operator claims it, awaiting one", async () => {
    const { container } = await renderPage();

    const scout = within(rowOf("Scout"));
    expect(scout.getByRole("link", { name: "Scout" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
    );
    expect(scout.getByText("9f2ac1b40d8e7a35")).toBeInTheDocument();
    expect(scout.getByText("Defined")).toBeInTheDocument();
    // No operator reported it, so no reported state is drawn — not even
    // a pending one, which is a state an operator reports.
    expect(scout.getByText("Awaiting operator")).toBeInTheDocument();
    expect(scout.queryByText("Provisioning")).not.toBeInTheDocument();
    // ADR-0011: the console prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("marks a definition cutting over or cut over to the Operator, and no other", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([
          { ...fieldAgent, cutover: { stage: "switched" } },
          {
            ...fieldAgent,
            agentGrn: "grn:acme:default:agent:0000000000000001",
            name: "Ranger",
            cutover: { stage: "frozen" },
          },
          {
            ...fieldAgent,
            agentGrn: "grn:acme:default:agent:0000000000000002",
            name: "Legacy",
            cutover: null,
          },
        ]),
      ),
    );

    await renderPage();

    expect(
      within(rowOf("Scout")).getByText("Managed by the Operator"),
    ).toBeInTheDocument();
    expect(
      within(rowOf("Ranger")).getByText("Cut-over in progress"),
    ).toBeInTheDocument();
    // The control: a definition with no open attempt is listed as
    // defined, so the marks above follow the stage and not the list.
    const legacy = within(rowOf("Legacy"));
    expect(legacy.getByText("Defined")).toBeInTheDocument();
    expect(legacy.queryByText("Managed by the Operator")).toBeNull();
    expect(legacy.queryByText("Cut-over in progress")).toBeNull();
  });

  it("shows a claimed agent's two reported states beside its name, each as its own dot", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([{ ...reportedAgent, grn: fieldAgent.agentGrn }]),
      ),
    );

    await renderPage();

    const scout = within(rowOf("Scout"));
    expect(scout.getByText("Provisioning failed")).toBeInTheDocument();
    expect(scout.getByText("Disconnected")).toBeInTheDocument();
    expect(scout.queryByText("Awaiting operator")).not.toBeInTheDocument();
  });

  it("lists an agent only an operator reported as unnamed, said to have no definition, with its identifier to copy", async () => {
    await renderPage();

    const reported = within(rowOf("1c3d5e7f90a2b4c6"));
    // A control service creates an agent with no definition in garam,
    // and its page is where its execution is read (#1170). Its
    // identifier names nothing, so it is not put where a name goes.
    expect(
      reported.getByRole("link", { name: "Unnamed agent" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/1c3d5e7f90a2b4c6");
    expect(
      reported.queryByRole("link", { name: "1c3d5e7f90a2b4c6" }),
    ).toBeNull();
    expect(
      reported.getByRole("button", {
        name: "Copy the identifier of Unnamed agent: 1c3d5e7f90a2b4c6",
      }),
    ).toBeInTheDocument();
    expect(
      reported.getByText("Reported by an operator without a definition here."),
    ).toBeInTheDocument();
    expect(reported.getByText("No definition")).toBeInTheDocument();
  });

  it("leads with failing agents and says how many are failing", async () => {
    const healthy = {
      ...reportedAgent,
      grn: fieldAgent.agentGrn,
      provisioningState: "ready",
      runtimeState: "connected",
    };
    // The control: with every reported agent healthy, nothing is said
    // to be failing and the lists' own order stands.
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([healthy]),
      ),
    );
    const calm = await renderPage();
    expect(screen.queryByText(/failing$/)).toBeNull();
    expect(listedNames()).toEqual(["Scout"]);
    calm.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([healthy, reportedAgent]),
      ),
    );
    await renderPage();

    expect(screen.getByRole("alert")).toHaveTextContent("1 agent is failing");
    // The unnamed reported agent comes after Scout in the lists, and is
    // listed first because it failed.
    expect(listedNames()).toEqual(["Unnamed agent", "Scout"]);
    // What is asserted is that the dots differ, not which colour either
    // one takes.
    expect(screen.getByText("Provisioning failed").dataset.tone).not.toEqual(
      screen.getByText("Ready").dataset.tone,
    );
    expect(screen.getByText("Disconnected").dataset.tone).not.toEqual(
      screen.getByText("Connected").dataset.tone,
    );
  });

  it("counts an agent failing on its runtime alone, its provisioning ready", async () => {
    // The control: ready and connected is not failing.
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            grn: fieldAgent.agentGrn,
            provisioningState: "ready",
            runtimeState: "connected",
          },
        ]),
      ),
    );
    const calm = await renderPage();
    expect(screen.queryByText(/is failing$/)).toBeNull();
    calm.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            grn: fieldAgent.agentGrn,
            provisioningState: "ready",
            runtimeState: "disconnected",
          },
        ]),
      ),
    );
    await renderPage();

    expect(screen.getByText("1 agent is failing")).toBeInTheDocument();
    expect(listedNames()[0]).toBe("Scout");
  });

  it("marks a cutover in progress apart from one finished and from a definition with none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([
          { ...fieldAgent, cutover: { stage: "frozen" } },
          {
            ...fieldAgent,
            agentGrn: "grn:acme:default:agent:0000000000000001",
            name: "Ranger",
            cutover: { stage: "switched" },
          },
          {
            ...fieldAgent,
            agentGrn: "grn:acme:default:agent:0000000000000002",
            name: "Legacy",
            cutover: null,
          },
        ]),
      ),
    );

    await renderPage();

    // What is asserted is that the markings differ, not which colour
    // each one takes.
    const frozen = screen.getByText("Cut-over in progress").dataset.tone;
    expect(frozen).not.toEqual(
      screen.getByText("Managed by the Operator").dataset.tone,
    );
    expect(frozen).not.toEqual(screen.getByText("Defined").dataset.tone);
  });

  it("names a defined agent's operator and links to it", async () => {
    await renderPage();

    expect(
      within(rowOf("Scout")).getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
  });

  it("falls back to the operator's identifier when operators are not listed", async () => {
    // The control: with operators listed, the operator is named and
    // linked.
    const allowed = await renderPage();
    expect(
      within(rowOf("Scout")).getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(refuse("operators"));

    await renderPage();

    const scout = within(rowOf("Scout"));
    expect(scout.getByText("field-one")).toBeInTheDocument();
    expect(
      scout.queryByRole("link", { name: "Field cluster one" }),
    ).not.toBeInTheDocument();
  });

  it("says reported state is not checked, rather than awaiting, when reports are refused", async () => {
    // The control: with reports listed, the agent awaits an operator
    // and nothing is said to be unchecked.
    const allowed = await renderPage();
    expect(
      within(rowOf("Scout")).getByText("Awaiting operator"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Not checked")).not.toBeInTheDocument();
    allowed.unmount();

    server.use(refuse("agents"));

    await renderPage();

    expect(
      screen.getByText("Reported agents are not listed to you"),
    ).toBeInTheDocument();
    const scout = within(rowOf("Scout"));
    expect(scout.getAllByText("Not checked")).toHaveLength(2);
    expect(scout.queryByText("Awaiting operator")).not.toBeInTheDocument();
    // A partial list is not counted as though it were whole.
    expect(
      within(screen.getByRole("heading", { level: 1 })).queryByText("1"),
    ).toBeNull();
  });

  it("says definitions are not listed rather than showing none", async () => {
    // The control: with definitions listed, the defined agent is shown,
    // both are counted and no refusal is said.
    const allowed = await renderPage();
    expect(screen.getByText("Scout")).toBeInTheDocument();
    expect(
      within(screen.getByRole("heading", { level: 1 })).getByText("2"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Agent definitions are not listed to you"),
    ).not.toBeInTheDocument();
    allowed.unmount();

    server.use(refuse("agent-definitions"));

    await renderPage();

    expect(
      screen.getByText("Agent definitions are not listed to you"),
    ).toBeInTheDocument();
    // The reported agent is still listed: the refusal narrows the page.
    // Its definition may be in the list that was refused, so it is not
    // said to have none.
    const reported = within(rowOf("1c3d5e7f90a2b4c6"));
    expect(
      reported.getByText(
        "Reported by an operator. Its definition is not checked.",
      ),
    ).toBeInTheDocument();
    expect(reported.queryByText("No definition")).toBeNull();
    // Nor is it called unnamed: its name is one the page did not read.
    expect(
      reported.getByRole("link", { name: "Name not available" }),
    ).toBeInTheDocument();
    expect(reported.queryByText("Unnamed agent")).toBeNull();
    expect(screen.queryByText("Scout")).not.toBeInTheDocument();
  });

  it("shows an empty state when the organization has no agents at all", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([]),
      ),
    );

    await renderPage();

    expect(screen.getByText("No agents")).toBeInTheDocument();
    expect(
      screen.getByText("This organization has no agents."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("offers creating an agent unless the server denies it", async () => {
    // The control: answered allowed, the link is offered.
    const allowed = await renderPage();
    expect(screen.getByRole("link", { name: "New agent" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/new",
    );
    allowed.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "agent:create" ? "deny" : "allow",
      ),
    );
    await renderPage();

    expect(screen.queryByRole("link", { name: "New agent" })).toBeNull();
    expect(screen.getByText("Scout")).toBeInTheDocument();
  });

  it("offers creating an agent when the server cannot answer whether it may", async () => {
    server.use(answerPermissionChecks(() => "unknown"));

    await renderPage();

    expect(screen.getByRole("link", { name: "New agent" })).toBeInTheDocument();
  });

  it.each([403, 404])(
    "renders not-found when the organization answers %i",
    async (status) => {
      server.use(
        http.get(`${API_URL}/orgs/acme`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status }),
        ),
      );

      await expect(renderPage()).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the API is unreachable rather than reporting no agents", async () => {
    server.use(http.all(`${API_URL}/*`, () => HttpResponse.error()));

    await renderPage();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No agents")).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(renderPage()).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
