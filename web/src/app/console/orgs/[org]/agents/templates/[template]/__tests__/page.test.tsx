import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentTemplatePage from "@/app/console/orgs/[org]/agents/templates/[template]/page";
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
  usePathname: () => "/console/orgs/acme/agents/templates/7c1e2a9b3d4f5a60",
}));

const ONE = `${API_URL}/orgs/acme/agent-templates/7c1e2a9b3d4f5a60`;
const params = {
  params: Promise.resolve({ org: "acme", template: "7c1e2a9b3d4f5a60" }),
};

/** What the template's own read answers for the rest of the test. */
function answer(status: 200 | 403 | 404) {
  server.use(
    http.get(ONE, () =>
      status === 200
        ? HttpResponse.json(researcher)
        : HttpResponse.json(
            {
              kind: status === 403 ? "permission_denied" : "not_found",
              message: "no",
            },
            { status },
          ),
    ),
  );
}

describe("agent template page", () => {
  it("shows the archived template as a record, with nothing to edit, delete or create from", async () => {
    await renderServerComponent(AgentTemplatePage(params));

    expect(
      screen.getByRole("heading", { level: 1, name: "Researcher" }),
    ).toBeInTheDocument();
    // The control: what the template said is on the page.
    expect(screen.getByText("Reads papers")).toBeInTheDocument();
    expect(screen.getByText("workspace")).toBeInTheDocument();
    expect(screen.getByText("papers")).toBeInTheDocument();
    expect(screen.getByText("notes")).toBeInTheDocument();
    // garam's templates are retired read-only (ADR-0088).
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Create agent from template" }),
    ).not.toBeInTheDocument();
  });

  it("says a refused template is refused rather than missing", async () => {
    // The control: answered, the same template renders under its name
    // and says nothing is refused.
    answer(200);
    const allowed = await renderServerComponent(AgentTemplatePage(params));
    expect(
      screen.getByRole("heading", { level: 1, name: "Researcher" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    allowed.unmount();

    answer(403);
    await renderServerComponent(AgentTemplatePage(params));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "garam refused to show this template to you.",
    );
  });

  it("renders not-found for a template the organization does not keep", async () => {
    // The control: answered, the same address renders the template.
    answer(200);
    const allowed = await renderServerComponent(AgentTemplatePage(params));
    expect(
      screen.getByRole("heading", { level: 1, name: "Researcher" }),
    ).toBeInTheDocument();
    allowed.unmount();

    answer(404);
    await expect(
      renderServerComponent(AgentTemplatePage(params)),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });
});
