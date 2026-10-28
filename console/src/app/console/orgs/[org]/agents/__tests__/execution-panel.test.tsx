import { render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ExecutionPanel } from "@/app/console/orgs/[org]/agents/_components/execution-panel";
import type { DelegatedOperator } from "@/features/orgs";
import { TooltipProvider } from "@/components/ui/tooltip";
import { server } from "@/testing/setup";

// The authority is minted by a Server Action; what the panel does with
// the control service's answer is the subject here.
vi.mock("@/features/orgs/actions", () => ({
  mintOperationAuthority: async () => ({
    status: "ok",
    authority: "test-authority",
  }),
}));

const ENDPOINT = "https://control.example.test/garam";
const AGENT = "grn:acme:default:agent:0123456789abcdef";
const GENERATION = "0123456789abcdef0123456789abcdef";

const hosted: DelegatedOperator = {
  grn: "grn:acme:default:operator:hosted",
  identifier: "hosted",
  name: "Hosted",
  endpoint: ENDPOINT,
  delegation: {
    grantId: "grant-1",
    operatorGrn: "grn:acme:default:operator:hosted",
    controllers: [],
    operations: ["agent:execution-read"],
    expiresAt: "2099-01-01T00:00:00Z",
    grantedBy: "grn:acme:default:user:alice",
    grantedAt: "2026-10-01T00:00:00Z",
  },
  problem: null,
};

describe("execution panel", () => {
  it("offers the running generation to copy under its own term, never inside a sentence", async () => {
    server.use(
      http.get(`${ENDPOINT}/v1/orgs/acme/agents/:agent/execution`, () =>
        HttpResponse.json({
          desired: { revision: "2", pending: false },
          rendered: { revision: "2" },
          effective: {
            revision: "2",
            generation: GENERATION,
            observedAt: "2026-10-14T10:00:00Z",
          },
        }),
      ),
    );
    render(<ExecutionPanel org="acme" agentGrn={AGENT} hosted={hosted} />, {
      wrapper: TooltipProvider,
    });

    const effective = (await screen.findByText("Effective")).nextElementSibling;
    // The control: the sentence is there, with the revision it reports.
    expect(effective).toHaveTextContent(/^Revision 2, reported/);
    expect(effective).not.toHaveTextContent(GENERATION);
    expect(screen.getByText("Generation").nextElementSibling).toHaveTextContent(
      GENERATION,
    );
    expect(
      screen.getByRole("button", {
        name: `Copy the identifier of the running agent's generation: ${GENERATION}`,
      }),
    ).toBeInTheDocument();
  });

  it("says a refused read in the console's words, not the control service's", async () => {
    server.use(
      http.get(`${ENDPOINT}/v1/orgs/acme/agents/:agent/execution`, () =>
        HttpResponse.json(
          { message: "agent has no revision" },
          { status: 404 },
        ),
      ),
    );
    render(<ExecutionPanel org="acme" agentGrn={AGENT} hosted={hosted} />, {
      wrapper: TooltipProvider,
    });

    expect(
      await screen.findByText(
        "The control service holds no revision for this agent, so it has no execution to show. Reload the page to check again.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/agent has no revision/)).toBeNull();
  });
});
