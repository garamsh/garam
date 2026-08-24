import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentsPage from "@/app/console/orgs/[org]/agents/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, fieldAgent, unauthenticated } from "@/testing/mocks/handlers";
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

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-definitions`, () =>
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
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(unauthenticated());

    await expect(
      renderServerComponent(AgentsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
