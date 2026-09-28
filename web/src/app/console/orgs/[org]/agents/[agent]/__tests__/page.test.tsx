import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentPage from "@/app/console/orgs/[org]/agents/[agent]/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, fieldAgent, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
}));

const ONE = `${API_URL}/orgs/acme/agent-definitions/9f2ac1b40d8e7a35`;
const params = {
  params: Promise.resolve({ org: "acme", agent: "9f2ac1b40d8e7a35" }),
};

describe("agent page", () => {
  it("names the agent, keeps what was requested apart from what was reported, and awaits an operator", async () => {
    const { container } = await renderServerComponent(AgentPage(params));

    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Identifier 9f2ac1b40d8e7a35")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Reported" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Awaiting operator")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Requested" }),
    ).toBeInTheDocument();
    expect(screen.getByText("haiku")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(
      screen.getByRole("link", { name: "Manage policies" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=agent%3A9f2ac1b40d8e7a35",
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows the states an operator and the agent reported once one claims it", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([{ ...reportedAgent, grn: fieldAgent.agentGrn }]),
      ),
    );

    await renderServerComponent(AgentPage(params));

    expect(screen.getByText("failed")).toBeInTheDocument();
    expect(screen.getByText("disconnected")).toBeInTheDocument();
    expect(screen.queryByText("Awaiting operator")).not.toBeInTheDocument();
  });

  it("renames the agent through its own form", async () => {
    let body: unknown;
    server.use(
      http.patch(ONE, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ ...fieldAgent, name: "Ranger" });
      }),
    );
    const user = userEvent.setup();
    await renderServerComponent(AgentPage(params));

    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), " Ranger ");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Renamed to Ranger.",
    );
    expect(body).toEqual({ name: "Ranger" });
  });

  it("says a refused definition is refused rather than missing", async () => {
    server.use(
      http.get(ONE, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderServerComponent(AgentPage(params));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "garam refused to show this agent to you.",
    );
  });
});
