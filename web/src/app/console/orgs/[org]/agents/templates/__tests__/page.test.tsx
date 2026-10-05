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
  it("lists each archived template by name as a record, and offers no new one", async () => {
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
    const archived = screen.getByRole("heading", {
      level: 2,
      name: "Archived garam templates",
    }).parentElement!;
    expect(
      within(archived).getByRole("link", { name: "Researcher" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/templates/7c1e2a9b3d4f5a60",
    );
    expect(within(archived).getByText("Reads papers")).toBeInTheDocument();
    // garam's templates are retired read-only (ADR-0088): the archived
    // list above is the control that the view still reads them.
    expect(
      screen.queryByRole("link", { name: "New template" }),
    ).not.toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("puts the control service's templates first, as the ones agents are created from", async () => {
    await renderServerComponent(AgentTemplatesPage(params));

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Control service templates", "Archived garam templates"]);
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

  it("shows an empty state when the organization kept none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json([]),
      ),
    );

    await renderServerComponent(AgentTemplatesPage(params));

    expect(screen.getByText("No archived templates")).toBeInTheDocument();
    expect(
      screen.getByText("This organization kept no templates in garam."),
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
    expect(screen.queryByText("No archived templates")).not.toBeInTheDocument();
  });
});
