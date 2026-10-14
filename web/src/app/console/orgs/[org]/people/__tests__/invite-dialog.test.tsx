import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { InviteDialog } from "@/app/console/orgs/[org]/people/_components/invite-dialog";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const INVITATIONS = `${API_URL}/orgs/acme/invitations`;

/** An invitation garam issued under `token`, as role `role`. */
function issued(token: string, role = "member") {
  return HttpResponse.json(
    {
      token,
      orgGrn: "grn:root:default:org:acme",
      role,
      expiresAt: "2026-01-09T03:04:05Z",
    },
    { status: 201 },
  );
}

/** Garam refusing the invitation to a caller without the right. */
function refused() {
  return HttpResponse.json(
    { kind: "permission_denied", message: "permission denied" },
    { status: 403 },
  );
}

async function open(user: UserEvent) {
  await user.click(screen.getByRole("button", { name: "Invite people" }));
  return screen.getByRole("dialog", { name: "Invite a person" });
}

async function chooseRole(user: UserEvent, role: string) {
  await user.click(screen.getByRole("combobox", { name: "Role" }));
  await user.click(await screen.findByRole("option", { name: role }));
}

function roleField() {
  return screen.getByRole("combobox", { name: "Role" });
}

describe("invite dialog", () => {
  it("reveals the link once garam issues it, naming the role it admits as", async () => {
    server.use(http.post(INVITATIONS, () => issued("secret-token", "admin")));
    const user = userEvent.setup();
    render(<InviteDialog org="acme" />);
    await open(user);

    await chooseRole(user, "Admin");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    const reveal = await screen.findByRole("alertdialog", {
      name: "Invitation link",
    });
    expect(
      within(reveal).getByText(
        "http://localhost:3000/console/invitations/secret-token",
      ),
    ).toBeInTheDocument();
    // Presence: the description goes on to say the link is shown once.
    expect(reveal).toHaveAccessibleDescription(
      /^It admits one person as Admin and works once\./,
    );
    // Shown once: nothing dismisses it before it is kept.
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("forgets the link with the dialog: reopening offers a new invitation", async () => {
    server.use(http.post(INVITATIONS, () => issued("secret-token")));
    const user = userEvent.setup();
    render(<InviteDialog org="acme" />);
    await open(user);
    await chooseRole(user, "Member");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    // The control: the link is on screen before the dialog closes.
    expect(
      await screen.findByText(
        "http://localhost:3000/console/invitations/secret-token",
      ),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Done" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    await open(user);

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(roleField()).toHaveTextContent("Choose a role");
    // Choosing the same role again brings back no old link.
    await chooseRole(user, "Member");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(screen.queryByText(/secret-token/)).not.toBeInTheDocument();
  });

  it("drops a refusal once the dialog closes", async () => {
    server.use(http.post(INVITATIONS, () => refused()));
    const user = userEvent.setup();
    render(<InviteDialog org="acme" />);
    await open(user);
    await chooseRole(user, "Admin");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    // The control: the refusal is the role's error while it is open.
    await waitFor(() =>
      expect(roleField()).toHaveAttribute("aria-invalid", "true"),
    );

    await user.click(screen.getByRole("button", { name: "Close" }));
    await open(user);

    expect(roleField()).not.toHaveAttribute("aria-invalid");
    expect(roleField()).toHaveTextContent("Choose a role");
  });

  it("attaches a refusal to the role and sends the same role again", async () => {
    let refusing = true;
    const roles: unknown[] = [];
    server.use(
      http.post(INVITATIONS, async ({ request }) => {
        roles.push(((await request.json()) as { role: string }).role);
        return refusing ? refused() : issued("secret-token", "admin");
      }),
    );
    const user = userEvent.setup();
    render(<InviteDialog org="acme" />);
    await open(user);

    await chooseRole(user, "Admin");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    await waitFor(() =>
      expect(roleField()).toHaveAttribute("aria-invalid", "true"),
    );
    expect(roleField()).toHaveAccessibleDescription(
      REFUSAL_COPY.permissionDenied,
    );
    expect(roleField()).toHaveTextContent("Admin");

    // The control: submitted again untouched, garam issues it as the
    // same role.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    await screen.findByRole("alertdialog", { name: "Invitation link" });
    expect(roles).toEqual(["admin", "admin"]);
  });

  it("says a role is needed when sent without one", async () => {
    const roles: unknown[] = [];
    server.use(
      http.post(INVITATIONS, async ({ request }) => {
        roles.push(((await request.json()) as { role: string }).role);
        return issued("secret-token");
      }),
    );
    const user = userEvent.setup();
    render(<InviteDialog org="acme" />);
    await open(user);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    await waitFor(() =>
      expect(roleField()).toHaveAccessibleDescription(
        "Choose a role for the invitation.",
      ),
    );
    expect(roles).toEqual([]);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("keeps the form and says so when the API never answers", async () => {
    server.use(http.post(INVITATIONS, () => HttpResponse.error()));
    const user = userEvent.setup();
    render(<InviteDialog org="acme" />);
    await open(user);

    await chooseRole(user, "Member");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    await waitFor(() =>
      expect(roleField()).toHaveAccessibleDescription(REFUSAL_COPY.unreachable),
    );
    expect(
      screen.getByRole("button", { name: "Create invitation" }),
    ).toBeInTheDocument();
  });
});
