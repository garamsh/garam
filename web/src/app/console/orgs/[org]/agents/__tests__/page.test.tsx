import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentsPage from "@/app/console/orgs/[org]/agents/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
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

function refuse(path: string) {
  return http.get(`${API_URL}/orgs/acme/${path}`, () =>
    HttpResponse.json(
      { kind: "permission_denied", message: "no" },
      { status: 403 },
    ),
  );
}

/** The card an agent is listed in, found by the text it carries. */
function cardOf(text: string): HTMLElement {
  const item = screen.getByText(text).closest("li");
  if (item === null) throw new Error(`no list item holds ${text}`);
  return item;
}

describe("agents section", () => {
  it("lists a defined agent by name before any operator claims it, awaiting one", async () => {
    const { container } = await renderServerComponent(
      AgentsPage(params("acme")),
    );

    const scout = cardOf("Scout");
    expect(within(scout).getByRole("link", { name: "Scout" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
    );
    expect(within(scout).getByText("9f2ac1b40d8e7a35")).toBeInTheDocument();
    // No operator reported it, so no reported state is drawn — not even
    // `pending`, which is a state an operator reports.
    expect(within(scout).getByText("Awaiting operator")).toBeInTheDocument();
    expect(within(scout).queryByText("pending")).not.toBeInTheDocument();
    // ADR-0011: the console prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows a claimed agent's reported states beside its name", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([{ ...reportedAgent, grn: fieldAgent.agentGrn }]),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    const scout = cardOf("Scout");
    expect(within(scout).getByText("failed")).toBeInTheDocument();
    expect(within(scout).getByText("disconnected")).toBeInTheDocument();
    expect(
      within(scout).queryByText("Awaiting operator"),
    ).not.toBeInTheDocument();
  });

  it("lists an agent only an operator reported, by identifier, said to have no definition", async () => {
    await renderServerComponent(AgentsPage(params("acme")));

    const reported = cardOf("1c3d5e7f90a2b4c6");
    // A control service creates an agent with no definition in garam,
    // and its page is where its execution is read (#1170).
    expect(
      within(reported).getByRole("link", { name: "1c3d5e7f90a2b4c6" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/1c3d5e7f90a2b4c6");
    expect(
      within(reported).getByText(
        "Reported by an operator without a definition here.",
      ),
    ).toBeInTheDocument();
    expect(within(reported).getByText("failed")).toBeInTheDocument();
  });

  it("marks a state that needs attention apart from one that does not", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          reportedAgent,
          {
            ...reportedAgent,
            grn: "grn:acme:default:agent:0a1b2c3d4e5f6071",
            provisioningState: "ready",
            runtimeState: "connected",
          },
        ]),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    // What is asserted is that the markings differ, not which colour
    // either one takes.
    expect(screen.getByText("failed").dataset.variant).not.toEqual(
      screen.getByText("ready").dataset.variant,
    );
    expect(screen.getByText("disconnected").dataset.variant).not.toEqual(
      screen.getByText("connected").dataset.variant,
    );
  });

  it("names a defined agent's operator and links to it", async () => {
    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      within(cardOf("Scout")).getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
  });

  it("falls back to the operator's identifier when operators are not listed", async () => {
    // The control: with operators listed, the operator is named and
    // linked.
    const allowed = await renderServerComponent(AgentsPage(params("acme")));
    expect(
      within(cardOf("Scout")).getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(refuse("operators"));

    await renderServerComponent(AgentsPage(params("acme")));

    const scout = cardOf("Scout");
    expect(within(scout).getByText("field-one")).toBeInTheDocument();
    expect(
      within(scout).queryByRole("link", { name: "Field cluster one" }),
    ).not.toBeInTheDocument();
  });

  it("says reported state is not checked, rather than awaiting, when reports are refused", async () => {
    // The control: with reports listed, the agent awaits an operator
    // and nothing is said to be unchecked.
    const allowed = await renderServerComponent(AgentsPage(params("acme")));
    expect(
      within(cardOf("Scout")).getByText("Awaiting operator"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/^Not checked:/)).not.toBeInTheDocument();
    allowed.unmount();

    server.use(refuse("agents"));

    await renderServerComponent(AgentsPage(params("acme")));

    const scout = cardOf("Scout");
    expect(
      within(scout).getByText(
        "Not checked: reported agents are not listed to you.",
      ),
    ).toBeInTheDocument();
    expect(
      within(scout).queryByText("Awaiting operator"),
    ).not.toBeInTheDocument();
  });

  it("says definitions are not listed rather than showing none", async () => {
    // The control: with definitions listed, the defined agent is shown
    // and no refusal is said.
    const allowed = await renderServerComponent(AgentsPage(params("acme")));
    expect(screen.getByText("Scout")).toBeInTheDocument();
    expect(
      screen.queryByText(/Agent definitions are not listed to you/),
    ).not.toBeInTheDocument();
    allowed.unmount();

    server.use(refuse("agent-definitions"));

    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      screen.getByText(
        "Agent definitions are not listed to you, so agents awaiting an operator are not shown.",
      ),
    ).toBeInTheDocument();
    // The reported agent is still listed: the refusal narrows the page.
    // Its definition may be in the list that was refused, so it is not
    // said to have none.
    const reported = cardOf("1c3d5e7f90a2b4c6");
    expect(
      within(reported).getByText(
        "Reported by an operator. Its definition is not checked.",
      ),
    ).toBeInTheDocument();
    expect(
      within(reported).queryByText(
        "Reported by an operator without a definition here.",
      ),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Scout")).not.toBeInTheDocument();
  });

  it("shows an empty state when the organization has no agents at all", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([]),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    expect(screen.getByText("No agents")).toBeInTheDocument();
    expect(
      screen.getByText("This organization has no agents."),
    ).toBeInTheDocument();
  });

  it("offers creating an agent on its own page and switching to templates", async () => {
    await renderServerComponent(AgentsPage(params("acme")));

    expect(screen.getByRole("link", { name: "New agent" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/new",
    );
    const views = screen.getByRole("navigation", { name: "Agents views" });
    expect(within(views).getByRole("link", { name: "Agents" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      within(views).getByRole("link", { name: "Templates" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/templates");
  });

  it.each([403, 404])(
    "renders not-found when the organization answers %i",
    async (status) => {
      server.use(
        http.get(`${API_URL}/orgs/acme`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status }),
        ),
      );

      await expect(
        renderServerComponent(AgentsPage(params("acme"))),
      ).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the API is unreachable rather than reporting no agents", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No agents")).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(AgentsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
