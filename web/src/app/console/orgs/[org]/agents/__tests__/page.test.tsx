import { screen } from "@testing-library/react";
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

describe("agents section", () => {
  it("lists an agent by identifier and the two states its parties report", async () => {
    const { container } = await renderServerComponent(
      AgentsPage(params("acme")),
    );

    expect(screen.getByText("1c3d5e7f90a2b4c6")).toBeInTheDocument();
    expect(screen.getByText("failed")).toBeInTheDocument();
    expect(screen.getByText("disconnected")).toBeInTheDocument();
    // ADR-0011: the console addresses a resource by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
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

    // The property is that the two are told apart without reading the
    // word, so what is asserted is that their markings differ — not
    // which colour either one takes.
    expect(screen.getByText("failed").dataset.variant).not.toEqual(
      screen.getByText("ready").dataset.variant,
    );
    expect(screen.getByText("disconnected").dataset.variant).not.toEqual(
      screen.getByText("connected").dataset.variant,
    );
  });

  it("says what Defined by holds rather than leaving a subject bare", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([
          {
            ...fieldAgent,
            definedBy: "grn:root:default:user:112727439469584391833",
          },
        ]),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    // The record carries a user GRN and no name beside it
    // (`api/openapi.yaml` §AgentDefinition), so the subject is shown
    // and the list says what it is. Presence: the note is one clause
    // of a longer sentence.
    expect(screen.getByText("112727439469584391833")).toBeInTheDocument();
    expect(
      screen.getByText(/Defined by is the user identifier/),
    ).toBeInTheDocument();
  });

  it("says the organization holds no agents rather than leaving the area blank", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () => HttpResponse.json([])),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      screen.getByText(
        "No operator has reported an agent to this organization.",
      ),
    ).toBeInTheDocument();
  });

  it("lists a definition by identifier, operator, values and who defined it", async () => {
    const { container } = await renderServerComponent(
      AgentsPage(params("acme")),
    );

    expect(screen.getByRole("heading", { name: "Agents" })).toBeInTheDocument();
    expect(screen.getByText("9f2ac1b40d8e7a35")).toBeInTheDocument();
    expect(screen.getByText("field-one")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("model")).toBeInTheDocument();
    expect(screen.getByText("haiku")).toBeInTheDocument();
    // ADR-0011: the console addresses a resource by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("names a definition's operator and links to it, with its identifier beside", async () => {
    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(
      screen.getByRole("option", { name: "Field cluster one (field-one)" }),
    ).toBeInTheDocument();
  });

  it("falls back to identifiers when the operators are not listed to the caller", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    // The page still renders its definitions: the refusal narrows what
    // is shown rather than hiding the section.
    expect(screen.getByText("9f2ac1b40d8e7a35")).toBeInTheDocument();
    expect(screen.getByText("field-one")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Field cluster one" }),
    ).not.toBeInTheDocument();
    expect(screen.getByLabelText("Operator")).toHaveRole("textbox");
  });

  it("says a definition carries no values rather than showing an empty block", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([{ ...fieldAgent, values: {} }]),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    expect(screen.getByText("No values.")).toBeInTheDocument();
  });

  it("shows an empty state rather than a bare list", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json([]),
      ),
    );

    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      screen.getByText("This organization has defined no agents."),
    ).toBeInTheDocument();
  });

  it("offers the define form to anyone who may read the section", async () => {
    await renderServerComponent(AgentsPage(params("acme")));

    expect(screen.getByLabelText("Operator")).toBeInTheDocument();
    expect(screen.getByLabelText("Key 1")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Define agent" }),
    ).toBeInTheDocument();
  });

  it.each([
    ["agents", 403],
    ["agents", 404],
    ["agent-definitions", 403],
    ["agent-definitions", 404],
  ] as const)("renders not-found on %s answering %i", async (path, status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/${path}`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(AgentsPage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than reporting no definitions", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(AgentsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has defined no agents."),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(
        "No operator has reported an agent to this organization.",
      ),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(AgentsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
