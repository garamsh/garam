import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ConnectWizard,
  type ConnectStep,
} from "@/app/console/orgs/[org]/operators/[operator]/connect/_components/connect-wizard";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import type { OperatorKey } from "@/features/orgs";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
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
  {
    problem = null,
    canReplace = true,
    isControlService = false,
  }: {
    problem?: "unreachable" | "forbidden" | null;
    canReplace?: boolean;
    isControlService?: boolean;
  } = {},
) {
  return (
    <TooltipProvider>
      <ConnectWizard
        org="acme"
        operator="field-one"
        name="Field one"
        clientId={problem === null ? CLIENT_ID : undefined}
        keys={keys}
        problem={problem}
        initialStep={initialStep}
        canReplace={canReplace}
        isControlService={isControlService}
        operatorHref="/console/orgs/acme/operators/field-one"
        operatorsHref="/console/orgs/acme/operators"
      />
    </TooltipProvider>
  );
}

/** Issues a first key through the wizard and closes its reveal as kept. */
async function issueAndKeep(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Issue key" }));
  const reveal = await screen.findByRole("alertdialog", {
    name: "Save the private key",
  });
  await user.click(within(reveal).getByRole("checkbox"));
  await user.click(within(reveal).getByRole("button", { name: "Done" }));
}

/** The step the stepper marks as the one in hand. */
function currentStep() {
  return within(screen.getByRole("list", { name: "Connect Field one steps" }))
    .getAllByRole("listitem")
    .find((item) => item.getAttribute("aria-current") === "step");
}

/** Answers every key issuance for field-one with `answer`. */
function answerIssuance(answer: () => Response) {
  server.use(
    http.post(`${API_URL}/orgs/acme/operators/field-one/keys`, answer),
  );
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
    expect(snippet).toHaveTextContent(`client-id='${CLIENT_ID}'`);
    expect(snippet).toHaveTextContent(`token-url='${TOKEN_URL}'`);
    expect(snippet).toHaveTextContent("private-key.pem=./field-one.pem");
    expect(snippet).not.toHaveTextContent("BEGIN PRIVATE KEY");
    expect(screen.getByRole("group", { name: "Client ID" })).toHaveTextContent(
      CLIENT_ID,
    );
  });

  it("says this garam issues no keys when it refuses with key_issuance_unconfigured", async () => {
    answerIssuance(() =>
      HttpResponse.json(
        {
          kind: "failed_precondition",
          message:
            "no operator key is issued: GARAM_MACHINE_TOKEN_URL is unset",
          reason: "key_issuance_unconfigured",
        },
        { status: 409 },
      ),
    );
    const user = userEvent.setup();
    render(wizard("issue"));

    await user.click(screen.getByRole("button", { name: "Issue key" }));

    expect(
      await screen.findByText("This garam doesn't issue keys"),
    ).toBeInTheDocument();
    // The control: it is not read as an outage.
    expect(screen.queryByText("garam couldn't issue a key")).toBeNull();
    expect(screen.queryByText(/no operator key is issued/)).toBeNull();
    expect(currentStep()).toHaveTextContent("Issue a key");
  });

  it("reads a 409 without that reason as the refusal it is, not as unconfigured", async () => {
    answerIssuance(() =>
      HttpResponse.json(
        {
          kind: "failed_precondition",
          message: "operator holds two live keys",
        },
        { status: 409 },
      ),
    );
    const user = userEvent.setup();
    render(wizard("issue"));

    await user.click(screen.getByRole("button", { name: "Issue key" }));

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByText("This garam doesn't issue keys")).toBeNull();
  });

  it.each([
    [
      "a bare 503",
      () =>
        HttpResponse.json(
          { kind: "unavailable", message: "internal error" },
          { status: 503 },
        ),
    ],
    ["a failed connection", () => HttpResponse.error()],
  ])(
    "says garam couldn't be reached on %s, not that it issues no keys",
    async (_, answer) => {
      answerIssuance(answer);
      const user = userEvent.setup();
      render(wizard("issue"));

      await user.click(screen.getByRole("button", { name: "Issue key" }));

      expect(
        await screen.findByText("garam couldn't issue a key"),
      ).toBeInTheDocument();
      // The control: the outage is not read as a garam with no keys.
      expect(screen.queryByText("This garam doesn't issue keys")).toBeNull();
      expect(currentStep()).toHaveTextContent("Issue a key");
    },
  );

  it("rotates beside a live key by default, and replaces it only once confirmed", async () => {
    const posted = recordIssuances();
    const user = userEvent.setup();
    render(wizard("issue", [fieldOneKey]));

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

  it("keeps the replacement open with garam's refusal, and stays on issuing", async () => {
    answerIssuance(() =>
      HttpResponse.json(
        { kind: "permission_denied", message: "denied: operator:reissue" },
        { status: 403 },
      ),
    );
    const user = userEvent.setup();
    render(wizard("issue", [fieldOneKey]));

    await user.click(screen.getByRole("radio", { name: /Replace now/ }));
    await user.click(screen.getByRole("button", { name: "Replace key now" }));
    const confirm = screen.getByRole("alertdialog", {
      name: "Replace Field one's key now?",
    });
    await user.click(
      within(confirm).getByRole("button", { name: "Replace key now" }),
    );

    expect(await within(confirm).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(confirm).toBeInTheDocument();
    await user.click(within(confirm).getByRole("button", { name: "Cancel" }));
    expect(currentStep()).toHaveTextContent("Issue a key");
  });

  it("resumes on placing a key issued earlier, without its private half or token URL", () => {
    render(wizard("place", [neverUsed]));

    expect(currentStep()).toHaveTextContent("Place the key");
    expect(
      screen.getByText("The private key was shown once"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Issue a replacement" }),
    ).toHaveAttribute("href", "?rotate");
    expect(
      screen.getByText(/kubectl create secret generic garam-credential/),
    ).toHaveTextContent(
      "token-url='<token URL shown when the key was issued>'",
    );
  });

  it("offers no replacement link to a caller garam denies re-issuing", () => {
    render(wizard("place", [neverUsed], { canReplace: false }));

    expect(
      screen.queryByRole("link", { name: "Issue a replacement" }),
    ).not.toBeInTheDocument();
    // The control: the notice itself still stands.
    expect(
      screen.getByText("The private key was shown once"),
    ).toBeInTheDocument();
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

    const hidden = vi
      .spyOn(document, "visibilityState", "get")
      .mockReturnValue("hidden");
    try {
      act(() => {
        document.dispatchEvent(new Event("visibilitychange"));
      });
      await act(() => vi.advanceTimersByTimeAsync(20_000));
      expect(router.refresh).toHaveBeenCalledTimes(1);
    } finally {
      hidden.mockRestore();
    }
  });

  it("offers only replacing once two keys are live, and says why", () => {
    const second: OperatorKey = { ...fieldOneKey, kid: "second-kid" };
    render(wizard("issue", [fieldOneKey, second]));

    const rotate = screen.getByRole("radio", { name: /Rotate/ });
    expect(rotate).toBeDisabled();
    expect(rotate).not.toBeChecked();
    expect(rotate).toHaveAccessibleDescription(/Two keys are already live/);
    expect(screen.getByRole("radio", { name: /Replace now/ })).toBeChecked();
    expect(
      screen.getByRole("button", { name: "Replace key now" }),
    ).toBeInTheDocument();
  });

  it("offers rotating while one key is live", () => {
    render(wizard("issue", [fieldOneKey]));

    expect(screen.getByRole("radio", { name: /Rotate/ })).toBeEnabled();
  });

  it("keeps the private key revealable when the render after issuing fails a read", async () => {
    recordIssuances();
    const user = userEvent.setup();
    const { rerender } = render(wizard("issue"));
    await user.click(screen.getByRole("button", { name: "Issue key" }));
    await screen.findByRole("alertdialog", { name: "Save the private key" });

    rerender(wizard("issue", [], { problem: "unreachable" }));

    expect(screen.getByText("Cannot reach the API")).toBeInTheDocument();
    expect(
      screen.getByRole("alertdialog", { name: "Save the private key" }),
    ).toHaveTextContent("BEGIN PRIVATE KEY");
  });

  it("stops waiting, and says so, when the key is revoked before it signs in", async () => {
    recordIssuances();
    const user = userEvent.setup();
    const { rerender } = render(wizard("issue"));
    await issueAndKeep(user);
    await user.click(screen.getByRole("button", { name: /placed the key/ }));
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });

    rerender(wizard("issue", [{ ...neverUsed, state: "revoked" }]));
    await act(() => vi.advanceTimersByTimeAsync(20_000));

    expect(screen.getByText("This key was revoked")).toBeInTheDocument();
    expect(router.refresh).not.toHaveBeenCalled();
    expect(
      screen.queryByText("Waiting for its first sign-in"),
    ).not.toBeInTheDocument();
  });

  it("says the operator is connected once its key has signed in, and offers the control service next", () => {
    const { rerender } = render(wizard("verify", [neverUsed]));

    rerender(wizard("verify", [fieldOneKey]));

    expect(screen.getByText("Field one is connected")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Connect the control service too" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators?connect=field-one");
    expect(currentStep()).toHaveTextContent("Verify");
  });

  it("offers no second identity once a control service is connected, since its record names its manager (#1636)", () => {
    render(wizard("verify", [fieldOneKey], { isControlService: true }));

    expect(screen.getByText("Field one is connected")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Connect the control service too" }),
    ).toBeNull();
  });
});
