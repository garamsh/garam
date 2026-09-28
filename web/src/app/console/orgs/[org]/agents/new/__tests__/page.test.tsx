import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import NewAgentPage from "@/app/console/orgs/[org]/agents/new/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, researcher } from "@/testing/mocks/handlers";
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
      within(starts).getByRole("link", { name: "Researcher 7c1e2a9b3d4f5a60" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/new?template=7c1e2a9b3d4f5a60",
    );
    expect(screen.getByLabelText("Key 1")).toHaveValue("");
  });

  it("copies the chosen template's values into the form and says the copy is one-time", async () => {
    await renderServerComponent(page("7c1e2a9b3d4f5a60"));

    expect(screen.getByLabelText("Key 1")).toHaveValue("workspace");
    expect(screen.getByLabelText("Value 1")).toHaveValue("papers");
    expect(screen.getByLabelText("Key 2")).toHaveValue("notes");
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

  it("tells two templates of one name apart by identifier", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json([
          researcher,
          {
            ...researcher,
            grn: "grn:acme:default:agent-template:0a1b2c3d4e5f6071",
          },
        ]),
      ),
    );

    const { container } = await renderServerComponent(page());

    const starts = screen.getByRole("navigation", { name: "Start from" });
    expect(
      within(starts)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual([
      "Blank",
      "Researcher 7c1e2a9b3d4f5a60",
      "Researcher 0a1b2c3d4e5f6071",
    ]);
    expect(container.textContent).not.toContain("grn:");
  });

  it("says the template list is refused and still copies a chosen template it can read", async () => {
    // The control: with the list answered, no refusal is said.
    const allowed = await renderServerComponent(page("7c1e2a9b3d4f5a60"));
    expect(
      screen.queryByText("Templates are not listed to you."),
    ).not.toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderServerComponent(page("7c1e2a9b3d4f5a60"));

    expect(
      screen.getByText("Templates are not listed to you."),
    ).toBeInTheDocument();
    // The chosen template answered on its own read, so its copy stands.
    expect(screen.getByLabelText("Key 1")).toHaveValue("workspace");
    expect(
      screen.getByText(
        "Copied from Researcher. Changing the template later does not change this agent.",
      ),
    ).toBeInTheDocument();
  });

  it("labels the settings as saved values without claiming they are applied", async () => {
    const { container } = await renderServerComponent(page());

    expect(screen.getByText("Advanced settings")).toBeInTheDocument();
    // Presence: the sentence goes on to warn about credentials.
    expect(
      screen.getByText(/Saved values; application is not reported\./),
    ).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/is running|deployed|Sherlock/i);
  });
});
