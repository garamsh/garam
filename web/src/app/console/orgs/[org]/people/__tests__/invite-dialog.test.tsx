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

async function openDialog(user: UserEvent) {
  render(<InviteDialog org="acme" />);
  await user.click(screen.getByRole("button", { name: "Invite people" }));
  return screen.getByRole("dialog", { name: "Invite a person" });
}

/** The link the dialog offers, read from its copy field. */
function linkShown() {
  return within(
    screen.getByRole("group", { name: "Invitation link" }),
  ).getByText(/console\/invitations/).textContent;
}

async function chooseRole(user: UserEvent, role: string) {
  await user.click(screen.getByRole("combobox", { name: "Role" }));
  await user.click(await screen.findByRole("option", { name: role }));
}

describe("invite dialog", () => {
  it("offers the link to copy once garam issues it", async () => {
    server.use(http.post(INVITATIONS, () => issued("secret-token")));
    const user = userEvent.setup();
    await openDialog(user);

    expect(
      screen.queryByRole("group", { name: "Invitation link" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    await screen.findByRole("group", { name: "Invitation link" });
    expect(linkShown()).toBe(
      "http://localhost:3000/console/invitations/secret-token",
    );
    expect(
      screen.getByRole("button", { name: "Copy invitation link" }),
    ).toBeInTheDocument();
  });

  it("stops reading Copied once a second invitation replaces the link", async () => {
    let count = 0;
    server.use(
      http.post(INVITATIONS, () => {
        count += 1;
        return issued(`token-${count}`);
      }),
    );
    const user = userEvent.setup();
    await openDialog(user);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    await screen.findByRole("group", { name: "Invitation link" });
    await user.click(
      screen.getByRole("button", { name: "Copy invitation link" }),
    );
    expect(await screen.findByText("Copied")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    // The field already holds token 1, so its text is the only thing
    // that marks token 2 as having arrived.
    await waitFor(() =>
      expect(linkShown()).toBe(
        "http://localhost:3000/console/invitations/token-2",
      ),
    );
    expect(screen.queryByText("Copied")).not.toBeInTheDocument();
  });

  it("sends the role chosen", async () => {
    const roles: unknown[] = [];
    server.use(
      http.post(INVITATIONS, async ({ request }) => {
        roles.push(((await request.json()) as { role: string }).role);
        return issued("secret-token", "admin");
      }),
    );
    const user = userEvent.setup();
    await openDialog(user);

    await chooseRole(user, "Admin");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    await screen.findByRole("group", { name: "Invitation link" });
    expect(roles).toEqual(["admin"]);
  });

  it("attaches a refusal to the role and sends the same role again", async () => {
    let refusing = true;
    const roles: unknown[] = [];
    server.use(
      http.post(INVITATIONS, async ({ request }) => {
        roles.push(((await request.json()) as { role: string }).role);
        return refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : issued("secret-token", "admin");
      }),
    );
    const user = userEvent.setup();
    await openDialog(user);

    // Not the starting role, so a reset to it shows.
    await chooseRole(user, "Admin");
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    const role = screen.getByRole("combobox", { name: "Role" });
    await waitFor(() => expect(role).toHaveAttribute("aria-invalid", "true"));
    expect(role).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied);
    expect(role).toHaveTextContent("Admin");
    expect(
      screen.queryByRole("group", { name: "Invitation link" }),
    ).not.toBeInTheDocument();

    // The control: submitted again untouched, garam issues it as the
    // same role and the error clears.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    await screen.findByRole("group", { name: "Invitation link" });
    expect(role).not.toHaveAttribute("aria-invalid");
    expect(roles).toEqual(["admin", "admin"]);
  });

  it("keeps the form and says so when the API never answers", async () => {
    server.use(http.post(INVITATIONS, () => HttpResponse.error()));
    const user = userEvent.setup();
    await openDialog(user);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    const role = screen.getByRole("combobox", { name: "Role" });
    await waitFor(() => expect(role).toHaveAttribute("aria-invalid", "true"));
    expect(role).toHaveAccessibleDescription(REFUSAL_COPY.unreachable);
    expect(
      screen.getByRole("button", { name: "Create invitation" }),
    ).toBeInTheDocument();
  });
});
