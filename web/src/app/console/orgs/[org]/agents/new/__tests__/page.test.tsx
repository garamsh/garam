import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import NewAgentPage from "@/app/console/orgs/[org]/agents/new/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  fieldOne,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
// The authority is minted by a Server Action, which the template step
// calls in the browser; nothing here reaches it.
vi.mock("@/features/orgs/actions", () => ({
  mintOperationAuthority: async () => ({ status: "unavailable" }),
}));

const ENDPOINT = "https://control.example.test/garam";
const HOSTED = "grn:acme:default:operator:hosted";

function page() {
  return NewAgentPage({ params: Promise.resolve({ org: "acme" }) });
}

/** The step the wizard marks as the one in hand. */
function currentStep(): HTMLElement | undefined {
  return within(screen.getByRole("list", { name: "New agent steps" }))
    .getAllByRole("listitem")
    .find((item) => item.getAttribute("aria-current") === "step");
}

/** A hosted operator whose grant carries `operations`. */
function hostedWith(operations: string[]) {
  return [
    http.get(`${API_URL}/orgs/acme/operators`, () =>
      HttpResponse.json([
        fieldOne,
        { ...fieldOne, grn: HOSTED, name: "Hosted", controlEndpoint: ENDPOINT },
      ]),
    ),
    http.get(`${API_URL}/orgs/acme/operators/hosted/delegation`, () =>
      HttpResponse.json({
        grantId: "grant-1",
        operatorGrn: HOSTED,
        controllers: [fieldOne.grn],
        operations,
        expiresAt: "2099-01-01T00:00:00Z",
        grantedBy: "grn:root:default:user:alice",
        grantedAt: "2026-10-01T00:00:00Z",
      }),
    ),
  ];
}

describe("new agent page", () => {
  it("offers no form that sends garam a definition or a template", async () => {
    // garam accepts no desired value (ADR-0090) and its templates left
    // the console (ADR-0116): the page reads neither and offers no
    // field to enter a value into.
    const read: string[] = [];
    server.events.on("request:start", ({ request }) => {
      read.push(new URL(request.url).pathname);
    });
    await renderServerComponent(page());
    server.events.removeAllListeners();

    expect(read).not.toContain("/orgs/acme/agent-templates");
    expect(read).not.toContain("/orgs/acme/agent-definitions");
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { level: 2, name: "Define in garam" }),
    ).not.toBeInTheDocument();
    // The control: the page did read garam, for the operators whose
    // control service it creates through.
    expect(read).toContain("/orgs/acme/operators");
  });

  it("is no dead end without a control service: it says why and links to where that is fixed", async () => {
    await renderServerComponent(page());

    expect(currentStep()).toHaveTextContent("Control service");
    expect(
      screen.getByText(/No control service is registered/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open operators" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators");
  });

  it("says why when the operators cannot be read, and links to no page the caller cannot open", async () => {
    // The control: with the operators read, the step links to them.
    const listed = await renderServerComponent(page());
    expect(
      screen.getByRole("link", { name: "Open operators" }),
    ).toBeInTheDocument();
    listed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );
    await renderServerComponent(page());

    expect(currentStep()).toHaveTextContent("Control service");
    expect(
      screen.getByText(/garam refused to list this organization's operators/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /^Open/ })).toBeNull();
  });

  it("links to the operator whose grant lacks creating", async () => {
    server.use(...hostedWith(["agent-template:read"]));

    await renderServerComponent(page());

    expect(currentStep()).toHaveTextContent("Control service");
    expect(
      screen.getByText(
        "Hosted's delegation does not include “Create agents from published templates”.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open the operator" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/hosted");
  });

  it("resumes at the template step once a control service can create", async () => {
    server.use(...hostedWith(["agent:create", "agent-template:read"]));

    await renderServerComponent(page());

    expect(currentStep()).toHaveTextContent("Template");
    expect(screen.queryByRole("link", { name: /^Open/ })).toBeNull();
  });

  it("says creating is denied where the server denies it", async () => {
    server.use(
      ...hostedWith(["agent:create", "agent-template:read"]),
      answerPermissionChecks((action) =>
        action === "agent:create" ? "deny" : "allow",
      ),
    );

    await renderServerComponent(page());

    expect(currentStep()).toHaveTextContent("Control service");
    expect(screen.getByText(REFUSAL_COPY.permissionDenied)).toBeInTheDocument();
  });

  it("lets creating through where the server cannot answer whether it may", async () => {
    server.use(
      ...hostedWith(["agent:create", "agent-template:read"]),
      answerPermissionChecks(() => "unknown"),
    );

    await renderServerComponent(page());

    expect(currentStep()).toHaveTextContent("Template");
    expect(screen.queryByText(REFUSAL_COPY.permissionDenied)).toBeNull();
  });
});
