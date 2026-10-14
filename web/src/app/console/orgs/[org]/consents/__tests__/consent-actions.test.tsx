import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { GrantConsentDialog } from "@/app/console/orgs/[org]/consents/_components/grant-consent-dialog";
import { GrantFromCard } from "@/app/console/orgs/[org]/consents/_components/grant-from-card";
import { WithdrawConsentDialog } from "@/app/console/orgs/[org]/consents/_components/withdraw-consent-dialog";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { enter } from "@/testing/enter";
import { API_URL, globexConsent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// list it refreshes is what reports the pair's new state.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const CONSENT = `${API_URL}/orgs/acme/consents/:other`;

/** Records each path the actions wrote, and answers as garam does. */
function record(method: "put" | "delete", answer: () => Response) {
  const written: string[] = [];
  server.use(
    http[method](CONSENT, ({ request }) => {
      written.push(new URL(request.url).pathname);
      return answer();
    }),
  );
  return written;
}

const denied = () =>
  HttpResponse.json(
    { kind: "permission_denied", message: "the admin role is required" },
    { status: 403 },
  );

describe("grant from a waiting card", () => {
  it("grants the card's pair without asking for the other organization", async () => {
    const granted = record("put", () => HttpResponse.json(globexConsent));
    const user = userEvent.setup();
    render(<GrantFromCard org="acme" other="globex" />);

    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Grant consent" }));

    await vi.waitFor(() =>
      expect(granted).toEqual(["/orgs/acme/consents/globex"]),
    );
  });

  it("says a refusal beside the button", async () => {
    record("put", denied);
    const user = userEvent.setup();
    render(<GrantFromCard org="acme" other="globex" />);

    await user.click(screen.getByRole("button", { name: "Grant consent" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
  });
});

describe("grant consent dialog", () => {
  async function grant(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: "New consent" }));
    const dialog = within(
      screen.getByRole("dialog", { name: "Grant consent" }),
    );
    await enter(user, dialog.getByLabelText("Other organization"), "globex");
    await user.click(dialog.getByRole("button", { name: "Grant consent" }));
    return dialog;
  }

  it("grants the half for the organization typed, closes, and says where the pair stands", async () => {
    const granted = record("put", () =>
      HttpResponse.json({
        ...globexConsent,
        grantedThere: false,
        stands: false,
      }),
    );
    const user = userEvent.setup();
    render(<GrantConsentDialog org="acme" />);

    await grant(user);

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Granted. Messages stay refused until globex grants its own half.",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(granted).toEqual(["/orgs/acme/consents/globex"]);
  });

  it("attaches a refusal to the field and keeps the identifier typed", async () => {
    record("put", denied);
    const user = userEvent.setup();
    render(<GrantConsentDialog org="acme" />);

    const dialog = await grant(user);

    const field = dialog.getByLabelText("Other organization");
    expect(
      await dialog.findByText(REFUSAL_COPY.permissionDenied),
    ).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveValue("globex");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});

describe("withdraw consent dialog", () => {
  async function ask(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: "Withdraw consent" }));
    return within(
      screen.getByRole("alertdialog", { name: "Withdraw consent for globex?" }),
    );
  }

  it("withdraws on the dialog's named button and not on the click that opens it", async () => {
    // The refusal and its control are read together: opening sends
    // nothing, and the same recorder shows the confirmed one sending.
    const withdrawn = record(
      "delete",
      () => new HttpResponse(null, { status: 204 }),
    );
    const user = userEvent.setup();
    render(<WithdrawConsentDialog org="acme" other="globex" stands />);

    const dialog = await ask(user);
    expect(withdrawn).toEqual([]);

    await user.click(dialog.getByRole("button", { name: "Withdraw consent" }));

    await vi.waitFor(() =>
      expect(withdrawn).toEqual(["/orgs/acme/consents/globex"]),
    );
    await vi.waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("sends nothing when cancelled", async () => {
    const withdrawn = record(
      "delete",
      () => new HttpResponse(null, { status: 204 }),
    );
    const user = userEvent.setup();
    render(<WithdrawConsentDialog org="acme" other="globex" stands />);

    const dialog = await ask(user);
    await user.click(dialog.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(withdrawn).toEqual([]);
  });

  it.each([
    [
      true,
      "Messages between this organization and globex stop at once. globex keeps its own half, so granting again here would let them cross.",
    ],
    [
      false,
      "Nothing crosses today, because globex has not granted its half, and withdrawing removes the relation.",
    ],
  ])("says what stops when the pair stands is %s", async (stands, said) => {
    const user = userEvent.setup();
    render(<WithdrawConsentDialog org="acme" other="globex" stands={stands} />);

    const dialog = await ask(user);

    expect(dialog.getByText(said)).toBeInTheDocument();
  });

  it("keeps the dialog open with the refusal in it", async () => {
    record("delete", denied);
    const user = userEvent.setup();
    render(<WithdrawConsentDialog org="acme" other="globex" stands />);

    const dialog = await ask(user);
    await user.click(dialog.getByRole("button", { name: "Withdraw consent" }));

    expect(await dialog.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });
});
