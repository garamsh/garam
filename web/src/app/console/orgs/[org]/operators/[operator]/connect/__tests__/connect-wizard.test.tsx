import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ConnectWizard,
  type ConnectStep,
} from "@/app/console/orgs/[org]/operators/[operator]/connect/_components/connect-wizard";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { OperatorKey } from "@/features/orgs";
import { API_URL, fieldOneKey } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const router = vi.hoisted(() => ({ refresh: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => router }));
// `revalidatePath` needs a request scope the test does not run in.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const CLIENT_ID = "grn:acme:default:operator:field-one";
const TOKEN_URL = "https://machine.garam.example/oauth/token";
const PEM = "-----BEGIN PRIVATE KEY-----\nMIIEvQ\n-----END PRIVATE KEY-----\n";
const neverUsed: OperatorKey = { ...fieldOneKey, lastExchangedAt: undefined };

afterEach(() => {
  router.refresh.mockClear();
  vi.useRealTimers();
});

/** Records every issuance the wizard posts, and answers as garam does. */
function recordIssuances() {
  const posted: unknown[] = [];
  server.use(
    http.post(
      `${API_URL}/orgs/acme/operators/field-one/keys`,
      async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(
          {
            key: neverUsed,
            privateKeyPem: PEM,
            clientId: CLIENT_ID,
            tokenUrl: TOKEN_URL,
          },
          { status: 201 },
        );
      },
    ),
  );
  return posted;
}

function wizard(
  initialStep: Exclude<ConnectStep, "register">,
  keys: OperatorKey[] = [],
  rotate = false,
) {
  return (
    <TooltipProvider>
      <ConnectWizard
        org="acme"
        operator="field-one"
        name="Field one"
        clientId={CLIENT_ID}
        keys={keys}
        initialStep={initialStep}
        rotate={rotate}
        operatorHref="/console/orgs/acme/operators/field-one"
        operatorsHref="/console/orgs/acme/operators"
      />
    </TooltipProvider>
  );
}

/** The step the stepper marks as the one in hand. */
function currentStep() {
  return within(screen.getByRole("list", { name: "Connect Field one steps" }))
    .getAllByRole("listitem")
    .find((item) => item.getAttribute("aria-current") === "step");
}

describe("connect wizard", () => {
  it("issues a first key, shows its private half once, and moves on to placing it", async () => {
    const posted = recordIssuances();
    const user = userEvent.setup();
    render(wizard("issue"));
    expect(currentStep()).toHaveTextContent("Issue a key");

    await user.click(screen.getByRole("button", { name: "Issue key" }));

    const reveal = await screen.findByRole("alertdialog", {
      name: "Save the private key",
    });
    expect(reveal).toHaveTextContent("BEGIN PRIVATE KEY");
    expect(posted).toEqual([{ rotation: "planned" }]);

    await user.click(within(reveal).getByRole("checkbox"));
    await user.click(within(reveal).getByRole("button", { name: "Done" }));

    expect(currentStep()).toHaveTextContent("Place the key");
    // The placement carries what the issuance answered, and the PEM
    // only as the file it was saved to.
    const snippet = screen.getByText(
      /kubectl create secret generic garam-credential/,
    );
    expect(snippet).toHaveTextContent(`client-id=${CLIENT_ID}`);
    expect(snippet).toHaveTextContent(`token-url=${TOKEN_URL}`);
    expect(snippet).toHaveTextContent("private-key.pem=./field-one.pem");
    expect(snippet).not.toHaveTextContent("BEGIN PRIVATE KEY");
    expect(
      screen.getByRole("group", {
        name: "Client ID (for the Operator's Secret)",
      }),
    ).toHaveTextContent(CLIENT_ID);
  });

  it("says garam couldn't issue a key, naming both causes, when it answers 503", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators/field-one/keys`, () =>
        HttpResponse.json(
          { kind: "unavailable", message: "GARAM_MACHINE_TOKEN_URL is unset" },
          { status: 503 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(wizard("issue"));

    await user.click(screen.getByRole("button", { name: "Issue key" }));

    expect(
      await screen.findByText("garam couldn't issue a key"),
    ).toBeInTheDocument();
    // Presence: the sentence names the outage and the configuration
    // both, since the seam cannot tell them apart (#1394).
    expect(
      screen.getByText(/isn.t\s+configured to issue keys/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/GARAM_MACHINE_TOKEN_URL/),
    ).not.toBeInTheDocument();
    expect(currentStep()).toHaveTextContent("Issue a key");
  });

  it("rotates beside a live key by default, and replaces it only once confirmed", async () => {
    const posted = recordIssuances();
    const user = userEvent.setup();
    render(wizard("issue", [fieldOneKey], true));

    expect(screen.getByRole("radio", { name: /Rotate/ })).toBeChecked();
    await user.click(screen.getByRole("radio", { name: /Replace now/ }));
    await user.click(screen.getByRole("button", { name: "Replace key now" }));
    const confirm = screen.getByRole("alertdialog", {
      name: "Replace Field one's key now?",
    });
    expect(posted).toEqual([]);

    await user.click(
      within(confirm).getByRole("button", { name: "Replace key now" }),
    );

    expect(
      await screen.findByRole("alertdialog", { name: "Save the private key" }),
    ).toBeInTheDocument();
    expect(posted).toEqual([{ rotation: "immediate" }]);
  });

  it("resumes on placing a key issued earlier, without its private half or token URL", () => {
    render(wizard("place", [neverUsed]));

    expect(currentStep()).toHaveTextContent("Place the key");
    expect(
      screen.getByText("The private key was shown when it was issued"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Issue a replacement" }),
    ).toHaveAttribute("href", "?rotate");
    expect(
      screen.getByText(/kubectl create secret generic garam-credential/),
    ).toHaveTextContent("token-url=<token URL shown when the key was issued>");
  });

  it("names the control service's Secret by placeholder", async () => {
    const user = userEvent.setup();
    render(wizard("place", [neverUsed]));

    await user.click(screen.getByRole("tab", { name: "Control service" }));

    expect(
      screen.getByText(
        /kubectl create secret generic <control-service-secret>/,
      ),
    ).toBeInTheDocument();
  });

  it("checks for the first sign-in while the step is visible, and stops when it is hidden", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    render(wizard("verify", [neverUsed]));
    expect(screen.getByRole("status")).toHaveTextContent(
      "Waiting for its first sign-in",
    );

    await act(() => vi.advanceTimersByTimeAsync(5_000));
    expect(router.refresh).toHaveBeenCalledTimes(1);

    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await act(() => vi.advanceTimersByTimeAsync(20_000));
    expect(router.refresh).toHaveBeenCalledTimes(1);
  });

  it("says the operator is connected once its key has signed in, and offers the control service next", () => {
    const { rerender } = render(wizard("verify", [neverUsed]));

    rerender(wizard("verify", [fieldOneKey]));

    expect(screen.getByText("Field one is connected")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Connect the control service too" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators?connect");
    expect(currentStep()).toHaveTextContent("Verify");
  });
});
