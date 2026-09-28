import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentTemplatesPage from "@/app/console/orgs/[org]/agents/templates/page";
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

const params = { params: Promise.resolve({ org: "acme" }) };

describe("agent templates view", () => {
  it("lists each template by name, linking to its page, inside Agents", async () => {
    const { container } = await renderServerComponent(
      AgentTemplatesPage(params),
    );

    expect(
      screen.getByRole("heading", { level: 1, name: "Agents" }),
    ).toBeInTheDocument();
    const views = screen.getByRole("navigation", { name: "Agents views" });
    expect(
      within(views).getByRole("link", { name: "Templates" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Researcher" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/templates/7c1e2a9b3d4f5a60",
    );
    expect(screen.getByText("Reads papers")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "New template" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/templates/new",
    );
    expect(container.textContent).not.toContain("grn:");
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

    const { container } = await renderServerComponent(
      AgentTemplatesPage(params),
    );

    const links = screen.getAllByRole("link", { name: "Researcher" });
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/console/orgs/acme/agents/templates/7c1e2a9b3d4f5a60",
      "/console/orgs/acme/agents/templates/0a1b2c3d4e5f6071",
    ]);
    expect(screen.getByText("7c1e2a9b3d4f5a60")).toBeInTheDocument();
    expect(screen.getByText("0a1b2c3d4e5f6071")).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows an empty state when the organization keeps none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json([]),
      ),
    );

    await renderServerComponent(AgentTemplatesPage(params));

    expect(screen.getByText("No templates")).toBeInTheDocument();
    expect(
      screen.getByText("This organization keeps no templates."),
    ).toBeInTheDocument();
  });

  it("says the list was refused rather than showing none", async () => {
    let refusing = false;
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "no" },
              { status: 403 },
            )
          : HttpResponse.json([researcher]),
      ),
    );
    // The control: answered, the same list names its template and
    // refuses nothing.
    const allowed = await renderServerComponent(AgentTemplatesPage(params));
    expect(
      screen.getByRole("link", { name: "Researcher" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    allowed.unmount();

    refusing = true;
    await renderServerComponent(AgentTemplatesPage(params));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "garam refused to show this organization's templates to you.",
    );
    expect(screen.queryByText("No templates")).not.toBeInTheDocument();
  });
});
