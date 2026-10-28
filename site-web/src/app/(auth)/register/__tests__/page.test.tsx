import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import RegisterPage from "@/app/(auth)/register/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, acmeInvitation, signedOut } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const NO_PARAMS = { searchParams: Promise.resolve({}) };

/** The page as reached from an invitation link the proxy redirected. */
function invitedTo(token: string) {
  return {
    searchParams: Promise.resolve({ next: `/console/invitations/${token}` }),
  };
}

describe("register page", () => {
  it("asks for an invitation rather than offering an open form", async () => {
    server.use(...signedOut());

    await renderServerComponent(RegisterPage(NO_PARAMS));

    expect(
      screen.getByRole("heading", { name: "Create your garam account" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Invitation token")).toBeRequired();
    expect(screen.getByLabelText("Username")).toBeRequired();
    expect(screen.getByLabelText("Password")).toBeRequired();
    expect(
      screen.getByRole("button", { name: "Create account" }),
    ).toBeInTheDocument();
  });

  it("sends a visitor who already holds a session to the console", async () => {
    // Registering again would mint a second account and sign this
    // browser into it; an invitation is accepted from the console
    // instead (ADR-0041).
    await expect(
      renderServerComponent(RegisterPage(NO_PARAMS)),
    ).rejects.toThrow(
      "NEXT_REDIRECT:http://console.garam.localhost:3000/console",
    );
  });

  it("says the API is unreachable instead of failing", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await renderServerComponent(RegisterPage(NO_PARAMS));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Invitation token")).not.toBeInTheDocument();
  });

  it("fills the invitation in when reached from an invitation link", async () => {
    server.use(...signedOut());

    const { container } = await renderServerComponent(
      RegisterPage(invitedTo("secret-token")),
    );

    expect(
      screen.getByRole("heading", {
        name: "Create an account to join Acme",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("Joining as an admin")).toBeInTheDocument();
    // Nobody copies the token by hand: the field is not asked for, and
    // the form carries the token the link held.
    expect(screen.queryByLabelText("Invitation token")).toBeNull();
    expect(
      container.querySelector('input[type="hidden"][name="invitationToken"]'),
    ).toHaveValue("secret-token");
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute(
      "href",
      "/login?next=%2Fconsole%2Finvitations%2Fsecret-token",
    );
  });

  it.each(["accepted", "expired"] as const)(
    "says an %s invitation can no longer be used instead of offering a form that would be refused",
    async (state) => {
      server.use(
        ...signedOut(),
        http.get(`${API_URL}/invitations/secret-token`, () =>
          HttpResponse.json({ ...acmeInvitation, state }),
        ),
      );

      await renderServerComponent(RegisterPage(invitedTo("secret-token")));

      expect(screen.getByRole("status")).toHaveTextContent(
        state === "accepted"
          ? "This invitation has already been used"
          : "This invitation has expired",
      );
      expect(
        screen.queryByRole("button", { name: "Create account" }),
      ).toBeNull();
    },
  );

  it("sends a signed-in visitor on to the invitation rather than registering again", async () => {
    await expect(
      renderServerComponent(RegisterPage(invitedTo("secret-token"))),
    ).rejects.toThrow(
      "NEXT_REDIRECT:http://console.garam.localhost:3000/console/invitations/secret-token",
    );
  });
});
