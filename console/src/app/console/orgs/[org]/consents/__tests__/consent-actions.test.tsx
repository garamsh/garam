import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConsentConfirmDialog } from "@/app/console/orgs/[org]/consents/_components/consent-confirm-dialog";
import { GrantConsentDialog } from "@/app/console/orgs/[org]/consents/_components/grant-consent-dialog";
import { grantOrgConsent, withdrawOrgConsent } from "@/features/orgs/actions";
import { REFUSAL_COPY } from "@garam/platform-client/refusal-copy";
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
const HEX = "3fa9c2d1e0b4a7f6";

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

const refused = (status: number, kind: string, message: string) => () =>
  HttpResponse.json({ kind, message }, { status });

const denied = refused(403, "permission_denied", "the admin role is required");

/** The two confirmations a card offers, as the card renders them. */
const ACTS = [
  {
    act: "Grant consent",
    method: "put",
    title: "Grant this organization's consent?",
    render: () => (
      <ConsentConfirmDialog
        org="acme"
        other={HEX}
        action={grantOrgConsent}
        act="Grant consent"
        doing="Granting…"
        title="Grant this organization's consent?"
        consequence="The other organization already holds its half, so messages cross between the two organizations in either direction from then on."
      />
    ),
    landed: () => HttpResponse.json(globexConsent),
  },
  {
    act: "Withdraw consent",
    method: "delete",
    title: "Withdraw this organization's consent?",
    render: () => (
      <ConsentConfirmDialog
        org="acme"
        other={HEX}
        action={withdrawOrgConsent}
        act="Withdraw consent"
        doing="Withdrawing…"
        title="Withdraw this organization's consent?"
        consequence="Messages between the two organizations stop at once."
        destructive
      />
    ),
    landed: () => new HttpResponse(null, { status: 204 }),
  },
] as const;

describe.each(ACTS)("$act confirmation", (subject) => {
  async function ask(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: subject.act }));
    return within(screen.getByRole("alertdialog", { name: subject.title }));
  }

  it("writes on the dialog's named button and not on the click that opens it", async () => {
    // The refusal and its control are read together: opening sends
    // nothing, and the same recorder shows the confirmed one sending.
    const written = record(subject.method, subject.landed);
    const user = userEvent.setup();
    render(subject.render());

    const dialog = await ask(user);
    expect(written).toEqual([]);

    await user.click(dialog.getByRole("button", { name: subject.act }));

    await vi.waitFor(() =>
      expect(written).toEqual([`/orgs/acme/consents/${HEX}`]),
    );
    await vi.waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("sends nothing when cancelled", async () => {
    const written = record(subject.method, subject.landed);
    const user = userEvent.setup();
    render(subject.render());

    const dialog = await ask(user);
    await user.click(dialog.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(written).toEqual([]);
  });

  it("names the pair by its identifier in mono only, never in its words", async () => {
    const user = userEvent.setup();
    render(subject.render());

    // Found by role alone, so a title carrying the identifier fails
    // the assertion below rather than the lookup.
    await user.click(screen.getByRole("button", { name: subject.act }));
    const alert = screen.getByRole("alertdialog");

    expect(
      within(alert).getByText(HEX, { selector: "code" }),
    ).toBeInTheDocument();
    expect(alert).not.toHaveAccessibleName(new RegExp(HEX));
    expect(alert).not.toHaveAccessibleDescription(new RegExp(HEX));
  });

  it("keeps the dialog open with the refusal in it", async () => {
    record(subject.method, denied);
    const user = userEvent.setup();
    render(subject.render());

    const dialog = await ask(user);
    await user.click(dialog.getByRole("button", { name: subject.act }));

    expect(await dialog.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });
});

describe("refusals a grant reads in its own words", () => {
  it.each([
    [
      "this organization itself",
      refused(
        400,
        "invalid_argument",
        "an organization consents with another, not with itself",
      ),
      REFUSAL_COPY.consentWithItself,
    ],
    [
      "an organization garam does not hold",
      refused(404, "not_found", "org not found"),
      REFUSAL_COPY.consentPartnerNotFound,
    ],
  ])("says so for %s, with no server text", async (_case, answer, said) => {
    record("put", answer);
    const user = userEvent.setup();
    render(<GrantConsentDialog org="acme" />);

    await user.click(screen.getByRole("button", { name: "New consent" }));
    const dialog = within(
      screen.getByRole("dialog", { name: "Grant consent" }),
    );
    await enter(user, dialog.getByLabelText("Other organization"), "acme");
    await user.click(dialog.getByRole("button", { name: "Grant consent" }));

    const field = dialog.getByLabelText("Other organization");
    await vi.waitFor(() =>
      expect(field).toHaveAccessibleDescription(
        `The identifier the other organization is addressed by. ${said}`,
      ),
    );
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveValue("acme");
  });

  it("leaves the same not-found to a withdrawal's generic sentence", async () => {
    // The control: the sentence is the grant's own, not a remap of
    // every 404 — the withdrawal reads the generic one.
    record("delete", refused(404, "not_found", "org not found"));
    const user = userEvent.setup();
    render(ACTS[1].render());

    await user.click(screen.getByRole("button", { name: "Withdraw consent" }));
    const dialog = within(screen.getByRole("alertdialog"));
    await user.click(dialog.getByRole("button", { name: "Withdraw consent" }));

    expect(await dialog.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.notFound,
    );
  });
});

describe("grant consent dialog", () => {
  async function grant(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: "New consent" }));
    const dialog = within(
      screen.getByRole("dialog", { name: "Grant consent" }),
    );
    await enter(user, dialog.getByLabelText("Other organization"), HEX);
    await user.click(dialog.getByRole("button", { name: "Grant consent" }));
    return dialog;
  }

  it("grants the half for the organization typed, closes, and says where the pair stands without its identifier", async () => {
    const granted = record("put", () =>
      HttpResponse.json({
        orgGrn: `grn:root:default:org:${HEX}`,
        grantedHere: true,
        grantedThere: false,
        stands: false,
      }),
    );
    const user = userEvent.setup();
    render(<GrantConsentDialog org="acme" />);

    await grant(user);

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Granted. Messages stay refused until the other organization grants its own half.",
    );
    expect(screen.getByRole("status")).not.toHaveTextContent(HEX);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(granted).toEqual([`/orgs/acme/consents/${HEX}`]);
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
    expect(field).toHaveValue(HEX);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
