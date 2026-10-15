import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { NewAgentWizard } from "@/app/console/orgs/[org]/agents/new/_components/new-agent-wizard";
import type { DelegatedOperator } from "@/features/orgs";
import { TooltipProvider } from "@/components/ui/tooltip";
import { server } from "@/testing/setup";

// The authority is minted by a Server Action; what the wizard does
// with the control service's answers is the subject here.
vi.mock("@/features/orgs/actions", () => ({
  mintOperationAuthority: async () => ({
    status: "ok",
    authority: "test-authority",
  }),
}));

const ENDPOINT = "https://control.example.test/garam";
const BASE = `${ENDPOINT}/v1/orgs/acme`;
const CONTROLLER = "grn:acme:default:operator:field-one";
const AGENT = "grn:acme:default:agent:0123456789abcdef";

function hosted(
  operations: DelegatedOperator["delegation"]["operations"],
): DelegatedOperator {
  return {
    grn: "grn:acme:default:operator:hosted",
    identifier: "hosted",
    name: "Hosted",
    endpoint: ENDPOINT,
    delegation: {
      grantId: "grant-1",
      operatorGrn: "grn:acme:default:operator:hosted",
      controllers: [CONTROLLER],
      operations,
      expiresAt: "2099-01-01T00:00:00Z",
      grantedBy: "grn:acme:default:user:alice",
      grantedAt: "2026-10-01T00:00:00Z",
    },
    problem: null,
  };
}

/** Creates through the wizard and answers what the step after asks. */
async function createWith(
  operations: DelegatedOperator["delegation"]["operations"],
) {
  const read: string[] = [];
  server.use(
    http.get(`${BASE}/templates`, () =>
      HttpResponse.json({
        templates: [
          {
            name: "minimax",
            version: 1,
            profile: { name: "small", version: 1 },
          },
        ],
      }),
    ),
    http.post(`${BASE}/agents`, () =>
      HttpResponse.json({ agent: AGENT, revision: "1", epoch: "1" }),
    ),
    http.get(`${BASE}/agents/:agent/execution`, () => {
      read.push("execution");
      return HttpResponse.json({
        desired: { revision: "1", pending: true },
        rendered: null,
        effective: null,
      });
    }),
  );
  const user = userEvent.setup();
  render(
    <NewAgentWizard
      org="acme"
      orgGrn="grn:root:default:org:acme"
      start={{ status: "ready", hosted: hosted(operations) }}
      controllers={{ [CONTROLLER]: "Field one" }}
    />,
    { wrapper: TooltipProvider },
  );
  for (const [label, option] of [
    ["Template", "minimax version 1"],
    ["Controller", "Field one"],
  ]) {
    await user.click(await screen.findByRole("combobox", { name: label }));
    await user.click(await screen.findByRole("option", { name: option }));
  }
  await user.click(
    screen.getByRole("button", { name: "Create from template" }),
  );
  await screen.findByText(/^Created at revision 1\./);
  return read;
}

function currentStep() {
  return within(screen.getByRole("list", { name: "New agent steps" }))
    .getAllByRole("listitem")
    .find((item) => item.getAttribute("aria-current") === "step");
}

describe("new agent wizard", () => {
  it("verifies the created agent by reading its execution back where the grant allows", async () => {
    const read = await createWith([
      "agent:create",
      "agent-template:read",
      "agent:execution-read",
    ]);

    expect(currentStep()).toHaveTextContent("Verify");
    expect(await screen.findByText("Revision 1 (pending)")).toBeInTheDocument();
    expect(read).toEqual(["execution"]);
  });

  it("names the last step for what it shows where the grant cannot read the agent back", async () => {
    const read = await createWith(["agent:create", "agent-template:read"]);

    expect(currentStep()).toHaveTextContent("Created");
    expect(currentStep()).not.toHaveTextContent("Verify");
    expect(
      screen.getByRole("group", { name: "Agent identifier" }),
    ).toHaveTextContent("0123456789abcdef");
    expect(read).toEqual([]);
  });
});
