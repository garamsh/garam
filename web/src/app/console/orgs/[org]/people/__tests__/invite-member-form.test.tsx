import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { InviteMemberForm } from "@/app/console/orgs/[org]/people/_components/invite-member-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("invite member form", () => {
  it("surfaces the copyable invitation link", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, () =>
        HttpResponse.json(
          {
            token: "secret-token",
            orgGrn: "grn:root:default:org:acme",
            role: "member",
            expiresAt: "2026-01-09T03:04:05Z",
          },
          { status: 201 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<InviteMemberForm org="acme" />);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    expect(await screen.findByLabelText("Invitation link")).toHaveValue(
      "http://localhost:3000/console/invitations/secret-token",
    );
  });

  it("stops reading Copied once a second invitation replaces the link", async () => {
    let issued = 0;
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, () => {
        issued += 1;
        return HttpResponse.json(
          {
            token: `token-${issued}`,
            orgGrn: "grn:root:default:org:acme",
            role: "member",
            expiresAt: "2026-01-09T03:04:05Z",
          },
          { status: 201 },
        );
      }),
    );
    const user = userEvent.setup();
    render(<InviteMemberForm org="acme" />);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    expect(await screen.findByLabelText("Invitation link")).toHaveValue(
      "http://localhost:3000/console/invitations/token-1",
    );
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(
      await screen.findByRole("button", { name: "Copied" }),
    ).toBeInTheDocument();

    // The second invitation is a different token nobody has copied yet.
    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    // The field already holds token 1, so its value is the only thing that
    // marks token 2 as having arrived.
    expect(
      await screen.findByDisplayValue(
        "http://localhost:3000/console/invitations/token-2",
      ),
    ).toHaveAccessibleName("Invitation link");
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Copied" }),
    ).not.toBeInTheDocument();
  });

  it("says the copy failed rather than leaving the button unchanged", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, () =>
        HttpResponse.json(
          {
            token: "secret-token",
            orgGrn: "grn:root:default:org:acme",
            role: "member",
            expiresAt: "2026-01-09T03:04:05Z",
          },
          { status: 201 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<InviteMemberForm org="acme" />);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));
    expect(await screen.findByLabelText("Invitation link")).toBeInTheDocument();

    // The control for the refusal: the same button, clicked the same
    // way, with the clipboard answering. Without it a missing "Copied"
    // is as much a handler that never ran as one that was refused.
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(
      await screen.findByRole("button", { name: "Copied" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    // A denied clipboard permission is what the browser answers with.
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new DOMException("Write permission denied.", "NotAllowedError"),
    );
    await user.click(screen.getByRole("button", { name: "Copied" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not copy the link",
    );
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Copied" }),
    ).not.toBeInTheDocument();
  });

  it("sends the role the owner picked", async () => {
    const roles: unknown[] = [];
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, async ({ request }) => {
        roles.push(((await request.json()) as { role: string }).role);
        return HttpResponse.json(
          {
            token: "secret-token",
            orgGrn: "grn:root:default:org:acme",
            role: "admin",
            expiresAt: "2026-01-09T03:04:05Z",
          },
          { status: 201 },
        );
      }),
    );
    const user = userEvent.setup();
    render(<InviteMemberForm org="acme" />);

    await user.click(screen.getByRole("combobox", { name: "Role" }));
    await user.click(screen.getByRole("option", { name: "Admin" }));
    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    expect(await screen.findByLabelText("Invitation link")).toBeInTheDocument();
    expect(roles).toEqual(["admin"]);
  });

  it("keeps the form when the API never answers", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, () => HttpResponse.error()),
    );
    const user = userEvent.setup();
    render(<InviteMemberForm org="acme" />);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create invitation" }),
    ).toBeInTheDocument();
  });

  it("shows the backend's error when creation fails", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/invitations`, () =>
        HttpResponse.json(
          { kind: "denied", message: "requires the admin role" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<InviteMemberForm org="acme" />);

    await user.click(screen.getByRole("button", { name: "Create invitation" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "requires the admin role",
    );
  });
});
