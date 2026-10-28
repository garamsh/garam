import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import type { ConnectStep } from "@/app/console/orgs/[org]/operators/[operator]/connect/_components/connect-wizard";
import { PairConnectWizard } from "@/app/console/orgs/[org]/operators/[operator]/connect/_components/pair-connect-wizard";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import type { OperatorKey } from "@/features/orgs";
import { API_URL, fieldOneKey } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const router = vi.hoisted(() => ({ refresh: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => router }));
// `revalidatePath` needs a request scope the test does not run in.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const MANAGER = "grn:acme:default:operator:aurora";
const CONTROL = "grn:acme:default:operator:aurora-control";
const TOKEN_URL = "https://machine.garam.example/oauth/token";
const neverUsed: OperatorKey = { ...fieldOneKey, lastExchangedAt: undefined };
const controlKey: OperatorKey = { ...neverUsed, kid: "control-kid" };

/**
 * Answers each identity's key issuance as garam does, each with its own
 * private key, and records which identities were issued one, in order.
 * `refuse` names an identity whose issuance garam refuses.
 */
function recordIssuances(refuse?: string) {
  const issued: string[] = [];
  server.use(
    http.post(`${API_URL}/orgs/acme/operators/:identity/keys`, ({ params }) => {
      const identity = String(params.identity);
      if (identity === refuse)
        return HttpResponse.json(
          { kind: "unavailable", message: "try again" },
          { status: 503 },
        );
      issued.push(identity);
      return HttpResponse.json(
        {
          key: identity === "aurora" ? neverUsed : controlKey,
          privateKeyPem: `-----BEGIN PRIVATE KEY-----\n${identity}\n-----END PRIVATE KEY-----\n`,
          clientId: `grn:acme:default:operator:${identity}`,
          tokenUrl: TOKEN_URL,
        },
        { status: 201 },
      );
    }),
  );
  return issued;
}

function wizard(
  initialStep: Exclude<ConnectStep, "register">,
  keys: { manager?: OperatorKey[]; control?: OperatorKey[] } = {},
) {
  return (
    <TooltipProvider>
      <PairConnectWizard
        org="acme"
        name="Aurora"
        identities={{
          manager: {
            operator: "aurora",
            name: "Aurora",
            clientId: MANAGER,
            keys: keys.manager ?? [],
          },
          control: {
            operator: "aurora-control",
            name: "aurora-control",
            clientId: CONTROL,
            keys: keys.control ?? [],
          },
        }}
        problem={null}
        initialStep={initialStep}
        canIssue
        operatorsHref="/console/orgs/acme/operators"
      />
    </TooltipProvider>
  );
}

/** Saves the private key a reveal shows and closes it as kept. */
async function keep(user: ReturnType<typeof userEvent.setup>, title: string) {
  const reveal = await screen.findByRole("alertdialog", { name: title });
  const shown = reveal.textContent ?? "";
  await user.click(within(reveal).getByRole("checkbox"));
  await user.click(within(reveal).getByRole("button", { name: "Done" }));
  return shown;
}

describe("pair connect wizard", () => {
  it("issues both keys in one step, shows each once as its own file, and places each in its own Secret (#1636)", async () => {
    const issued = recordIssuances();
    const user = userEvent.setup();
    render(wizard("issue"));

    await user.click(screen.getByRole("button", { name: "Issue both keys" }));

    const managerKey = await keep(
      user,
      "Save the cluster manager's private key",
    );
    expect(managerKey).toContain("aurora\n");
    const controlPem = await keep(
      user,
      "Save the control service's private key",
    );
    expect(controlPem).toContain("aurora-control");
    expect(issued).toEqual(["aurora", "aurora-control"]);

    const manager = screen.getByRole("region", {
      name: "The cluster manager's key",
    });
    expect(
      within(manager).getByText(
        /kubectl create secret generic garam-credential/,
      ),
    ).toHaveTextContent("private-key.pem=./aurora.pem");
    expect(within(manager).getByText(/kubectl create/)).toHaveTextContent(
      `client-id='${MANAGER}'`,
    );
    const control = screen.getByRole("region", {
      name: "The control service's key",
    });
    expect(within(control).getByText(/kubectl create/)).toHaveTextContent(
      "private-key.pem=./aurora-control.pem",
    );
    expect(within(control).getByText(/kubectl create/)).toHaveTextContent(
      `client-id='${CONTROL}'`,
    );
  });

  it("issues only the key still missing when one identity already holds a live key", async () => {
    const issued = recordIssuances();
    const user = userEvent.setup();
    render(wizard("issue", { manager: [neverUsed] }));

    await user.click(
      screen.getByRole("button", { name: "Issue the control service's key" }),
    );
    await keep(user, "Save the control service's private key");

    expect(issued).toEqual(["aurora-control"]);
    expect(
      screen.queryByRole("alertdialog", {
        name: "Save the cluster manager's private key",
      }),
    ).toBeNull();
  });

  it("says a refused issuance and offers the missing key again, keeping the one already issued", async () => {
    const issued = recordIssuances("aurora-control");
    const user = userEvent.setup();
    render(wizard("issue"));

    await user.click(screen.getByRole("button", { name: "Issue both keys" }));
    await keep(user, "Save the cluster manager's private key");

    expect(await screen.findByText("garam couldn't issue a key")).toBeVisible();
    expect(issued).toEqual(["aurora"]);
    // The control: the retry asks for the one key still missing.
    expect(
      screen.getByRole("button", { name: "Issue the control service's key" }),
    ).toBeEnabled();
  });

  it("waits for both identities to sign in before saying the Operator is connected", () => {
    const signedIn = fieldOneKey;
    const { rerender } = render(
      wizard("verify", {
        manager: [signedIn],
        control: [{ ...controlKey }],
      }),
    );

    expect(screen.queryByText("Aurora is connected")).toBeNull();
    expect(screen.getAllByText("Waiting for its first sign-in")).toHaveLength(
      1,
    );

    rerender(
      wizard("verify", {
        manager: [signedIn],
        control: [{ ...controlKey, lastExchangedAt: "2026-10-20T05:00:00Z" }],
      }),
    );

    expect(screen.getByText("Aurora is connected")).toBeInTheDocument();
    expect(screen.queryByText("Waiting for its first sign-in")).toBeNull();
  });
});
