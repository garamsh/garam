import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentTemplatesPage from "@/app/console/orgs/[org]/agents/templates/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  researcher,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { fieldOne } from "@/testing/mocks/handlers";

// The control service is read in the browser under an authority a
// Server Action mints; the page's wiring is the subject here.
vi.mock("@/features/orgs/actions", () => ({
  mintOperationAuthority: async () => ({
    status: "ok",
    authority: "test-authority",
  }),
}));

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const params = { params: Promise.resolve({ org: "acme" }) };

function renderPage() {
  return renderServerComponent(AgentTemplatesPage(params), {
    wrapper: TooltipProvider,
  });
}

describe("agent templates view", () => {
  it("lists each archived template by name as a record, and offers no new one", async () => {
    const { container } = await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: "Templates" }),
    ).toBeInTheDocument();
    const archived = screen.getByRole("region", {
      name: /^Archived garam templates/,
    });
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
    await renderPage();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Control service templates", "Archived garam templates1"]);
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

    const { container } = await renderPage();

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

    await renderPage();

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
    const allowed = await renderPage();
    expect(
      screen.getByRole("link", { name: "Researcher" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Archived templates are not listed to you"),
    ).not.toBeInTheDocument();
    allowed.unmount();

    refusing = true;
    await renderPage();

    expect(
      screen.getByText("Archived templates are not listed to you"),
    ).toBeInTheDocument();
    expect(screen.queryByText("No archived templates")).not.toBeInTheDocument();
  });

  it("says a missing control service in its own section and links to where it is fixed", async () => {
    await renderPage();

    const control = screen.getByRole("region", {
      name: "Control service templates",
    });
    expect(
      within(control).getByText(/No control service is registered/),
    ).toBeInTheDocument();
    expect(
      within(control).getByRole("link", { name: "Open operators" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators");
    // The archived list is unaffected.
    expect(
      screen.getByRole("link", { name: "Researcher" }),
    ).toBeInTheDocument();
  });

  it("asks the server about publishing", async () => {
    const asked: string[] = [];
    server.use(
      answerPermissionChecks((action) => {
        asked.push(action);
        return "allow";
      }),
    );

    await renderPage();

    expect(asked).toEqual(["agent-template:publish"]);
  });

  it("leaves Publish out where the server denies publishing, and offers it where it allows or cannot answer", async () => {
    const endpoint = "https://control.example.test/garam";
    const hosted = "grn:acme:default:operator:hosted";
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          {
            ...fieldOne,
            grn: hosted,
            name: "Hosted",
            controlEndpoint: endpoint,
          },
        ]),
      ),
      http.get(`${API_URL}/orgs/acme/operators/hosted/delegation`, () =>
        HttpResponse.json({
          grantId: "grant-1",
          operatorGrn: hosted,
          controllers: [fieldOne.grn],
          operations: ["agent-template:read", "agent-template:publish"],
          expiresAt: "2099-01-01T00:00:00Z",
          grantedBy: "grn:root:default:user:alice",
          grantedAt: "2026-10-01T00:00:00Z",
        }),
      ),
      http.get(`${endpoint}/v1/orgs/acme/templates`, () =>
        HttpResponse.json({ templates: [] }),
      ),
    );
    // The control: allowed, the page offers publishing.
    const allowed = await renderPage();
    expect(
      await screen.findByRole("button", { name: "Publish version" }),
    ).toBeInTheDocument();
    allowed.unmount();

    // Unanswered, the page still offers it and the server decides.
    server.use(answerPermissionChecks(() => "unknown"));
    const unanswered = await renderPage();
    expect(
      await screen.findByRole("button", { name: "Publish version" }),
    ).toBeInTheDocument();
    unanswered.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "agent-template:publish" ? "deny" : "allow",
      ),
    );
    await renderPage();

    expect(
      await screen.findByText("No template is published."),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Publish version" }),
    ).toBeNull();
  });
});
