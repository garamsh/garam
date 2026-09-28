import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentTemplatesPage from "@/app/console/orgs/[org]/agents/templates/page";
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

  it("shows an empty state when the organization keeps none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json([]),
      ),
    );

    await renderServerComponent(AgentTemplatesPage(params));

    expect(screen.getByText("No templates")).toBeInTheDocument();
  });

  it("says the list was refused rather than showing none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agent-templates`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderServerComponent(AgentTemplatesPage(params));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "garam refused to show this organization's templates to you.",
    );
    expect(screen.queryByText("No templates")).not.toBeInTheDocument();
  });
});
