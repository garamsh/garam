import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import NewAgentPage from "@/app/console/orgs/[org]/agents/new/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function page(template?: string) {
  return NewAgentPage({
    params: Promise.resolve({ org: "acme" }),
    searchParams: Promise.resolve(template === undefined ? {} : { template }),
  });
}

describe("new agent page", () => {
  it("starts blank and offers each template as a starting point", async () => {
    await renderServerComponent(page());

    const starts = screen.getByRole("navigation", { name: "Start from" });
    expect(within(starts).getByRole("link", { name: "Blank" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      within(starts).getByRole("link", { name: "Researcher" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/new?template=7c1e2a9b3d4f5a60",
    );
    expect(screen.getByLabelText("Key 1")).toHaveValue("");
  });

  it("copies the chosen template's values into the form and says the copy is one-time", async () => {
    await renderServerComponent(page("7c1e2a9b3d4f5a60"));

    expect(screen.getByLabelText("Key 1")).toHaveValue("model");
    expect(screen.getByLabelText("Value 1")).toHaveValue("sonnet");
    expect(screen.getByLabelText("Key 2")).toHaveValue("ego");
    expect(
      screen.getByText(
        "Copied from Researcher. Changing the template later does not change this agent.",
      ),
    ).toBeInTheDocument();
    // The name is the agent's own: nothing is copied into it.
    expect(screen.getByLabelText("Name")).toHaveValue("");
    // The operator is chosen by name with its identifier beside it.
    expect(
      screen.getByRole("option", { name: "Field cluster one (field-one)" }),
    ).toBeInTheDocument();
  });

  it("offers a blank start only, and says why, when templates are refused", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderServerComponent(page());

    expect(
      screen.getByText(
        "Templates are not listed to you, so only a blank start is offered.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Blank" })).toBeInTheDocument();
  });

  it("names Sherlock as what the settings are for without claiming anything runs", async () => {
    const { container } = await renderServerComponent(page());

    expect(screen.getByText("Sherlock settings")).toBeInTheDocument();
    expect(
      screen.getByText(/garam does not confirm the operator applies them/),
    ).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/is running|deployed/i);
  });
});
