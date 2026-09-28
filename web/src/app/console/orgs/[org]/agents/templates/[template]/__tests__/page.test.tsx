import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentTemplatePage from "@/app/console/orgs/[org]/agents/templates/[template]/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, researcher } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
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
  it("opens the template's fields for editing and offers creating an agent from it", async () => {
    await renderServerComponent(AgentTemplatePage(params));

    expect(
      screen.getByRole("heading", { level: 1, name: "Researcher" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("Researcher");
    expect(screen.getByLabelText("Description")).toHaveValue("Reads papers");
    expect(screen.getByLabelText("Key 1")).toHaveValue("workspace");
    expect(screen.getByLabelText("Value 1")).toHaveValue("papers");
    expect(
      screen.getByRole("link", { name: "Create agent from template" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/new?template=7c1e2a9b3d4f5a60",
    );
  });

  it("saves an edit as the whole template and says it did", async () => {
    let body: unknown;
    server.use(
      http.put(ONE, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ ...researcher, name: "Analyst" });
      }),
    );
    const user = userEvent.setup();
    await renderServerComponent(AgentTemplatePage(params));

    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "Analyst");
    await user.click(screen.getByRole("button", { name: "Save template" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Saved Analyst.",
    );
    expect(body).toEqual({
      name: "Analyst",
      description: "Reads papers",
      values: { workspace: "papers", notes: "weekly" },
    });
  });

  it("says deleting leaves agents created from it unchanged", async () => {
    const user = userEvent.setup();
    await renderServerComponent(AgentTemplatePage(params));

    await user.click(screen.getByRole("button", { name: "Delete template" }));

    expect(
      screen.getByText(
        "Delete Researcher? Agents created from it are not changed. garam cannot bring the template back.",
      ),
    ).toBeInTheDocument();
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
