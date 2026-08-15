import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { InviteMemberForm } from "@/app/console/orgs/[org]/_components/invite-member-form";
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

    await user.click(
      screen.getByRole("button", { name: "Create invitation" }),
    );

    expect(await screen.findByLabelText("Invitation link")).toHaveValue(
      "http://localhost:3000/console/invitations/secret-token",
    );
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

    await user.click(
      screen.getByRole("button", { name: "Create invitation" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "requires the admin role",
    );
  });
});
