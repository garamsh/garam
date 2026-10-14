import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import LoginPage from "@/app/(auth)/login/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  acmeInvitation,
  signedOutWith,
} from "@/testing/mocks/handlers";
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

describe("login page", () => {
  it("offers the Google sign-in button while signed out", async () => {
    server.use(...signedOutWith(["google"]));

    await renderServerComponent(LoginPage(NO_PARAMS));

    expect(
      screen.getByRole("heading", { name: "Sign in to garam" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toHaveAttribute("href", "/api/auth/login/google");
    expect(
      screen.getByRole("link", { name: "Back to the home page" }),
    ).toHaveAttribute("href", "/");
  });

  it("offers the username and password form beside the provider buttons", async () => {
    server.use(...signedOutWith(["google"]));

    await renderServerComponent(LoginPage(NO_PARAMS));

    expect(screen.getByLabelText("Username")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveAttribute(
      "type",
      "password",
    );
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toBeInTheDocument();
  });

  // The three tests below assert a refusal, so each carries the control
  // that isolates the mechanism in the same test (`testing.md` §Failing):
  // the same page, the same query, with the backend registering the
  // provider. Without it an absent button could be a page that failed to
  // render or a query that never matched anything.
  it("offers a provider button, and names Google, only when the backend registers one", async () => {
    server.use(...signedOutWith(["google"]));
    const registered = await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toBeInTheDocument();
    // Presence: the clause sits in a paragraph that also names the
    // registered account and how accounts are created.
    expect(
      screen.getByText(/Google account your organization invited/),
    ).toBeInTheDocument();
    registered.unmount();

    server.use(...signedOutWith([]));
    await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.queryByRole("link", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
    // Presence, and wide on purpose: the claim is that no element's
    // text names the provider, which the paragraph above would satisfy
    // by matching whole while still naming it.
    expect(screen.queryByText(/Google/)).not.toBeInTheDocument();
    // The password form is the way in that remains, so a deployment
    // that configures no provider is still usable.
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByText("or")).not.toBeInTheDocument();
  });

  it("offers no button for a provider it holds none for", async () => {
    server.use(...signedOutWith(["google"]));
    const known = await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toBeInTheDocument();
    known.unmount();

    server.use(...signedOutWith(["github"]));
    await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.queryByRole("link", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("offers no provider button when the provider list cannot be read", async () => {
    server.use(...signedOutWith(["google"]));
    const readable = await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toBeInTheDocument();
    readable.unmount();

    server.use(
      ...signedOutWith([]),
      http.get(`${API_URL}/auth/providers`, () => HttpResponse.error()),
    );
    await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.queryByRole("link", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  // The account clause renders inside the provider branch and the
  // membership clause outside it, so the same page with the provider
  // registered is the control: it is where the account clause has to
  // appear for its absence below to be the branch (`testing.md`
  // §Failing).
  it("says what a sign-in creates only where a provider is registered, and says what an invitation joins either way", async () => {
    server.use(...signedOutWith(["google"]));
    const registered = await renderServerComponent(LoginPage(NO_PARAMS));
    // Presence: each clause shares a paragraph with the others.
    expect(
      screen.getByText(/Signing in with Google creates an account/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Membership in an organization is by invitation/),
    ).toBeInTheDocument();
    registered.unmount();

    server.use(...signedOutWith([]));
    await renderServerComponent(LoginPage(NO_PARAMS));
    expect(
      screen.queryByText(/Signing in with Google creates an account/),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/Membership in an organization is by invitation/),
    ).toBeInTheDocument();
  });

  it("sends a visitor holding an invitation to the registration page", async () => {
    server.use(...signedOutWith(["google"]));

    await renderServerComponent(LoginPage(NO_PARAMS));

    expect(screen.getByRole("link", { name: "Create one" })).toHaveAttribute(
      "href",
      "/register",
    );
  });

  it("says the API is unreachable instead of failing", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await renderServerComponent(LoginPage(NO_PARAMS));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
  });

  it("says the API is unreachable when a dependency garam needs is down", async () => {
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          { kind: "unavailable", message: "internal error" },
          { status: 503 },
        ),
      ),
    );

    await renderServerComponent(LoginPage(NO_PARAMS));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Sign in" }),
    ).not.toBeInTheDocument();
  });

  it("sends an already signed-in visitor to the console", async () => {
    await expect(renderServerComponent(LoginPage(NO_PARAMS))).rejects.toThrow(
      "NEXT_REDIRECT:/console",
    );
  });

  it("sends an already signed-in visitor on to where they were going", async () => {
    await expect(
      renderServerComponent(LoginPage(invitedTo("secret-token"))),
    ).rejects.toThrow("NEXT_REDIRECT:/console/invitations/secret-token");
  });

  // The control is the console path the same parameter carries above:
  // only a path outside the console is dropped.
  it("drops a return path that leaves the console", async () => {
    await expect(
      renderServerComponent(
        LoginPage({
          searchParams: Promise.resolve({ next: "//evil.example/console" }),
        }),
      ),
    ).rejects.toThrow(/^NEXT_REDIRECT:\/console$/);
  });

  it("names the invitation it was reached for and carries it to sign-in and to register", async () => {
    server.use(...signedOutWith([]));

    const { container } = await renderServerComponent(
      LoginPage(invitedTo("secret-token")),
    );

    expect(
      screen.getByRole("heading", { name: "Sign in to join Acme" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Joining as an admin")).toBeInTheDocument();
    expect(
      container.querySelector('input[type="hidden"][name="next"]'),
    ).toHaveValue("/console/invitations/secret-token");
    expect(screen.getByRole("link", { name: "Create one" })).toHaveAttribute(
      "href",
      "/register?next=%2Fconsole%2Finvitations%2Fsecret-token",
    );
  });

  it("says an invitation can no longer be used, and still offers to sign in", async () => {
    server.use(
      ...signedOutWith([]),
      http.get(`${API_URL}/invitations/secret-token`, () =>
        HttpResponse.json({ ...acmeInvitation, state: "expired" }),
      ),
    );

    await renderServerComponent(LoginPage(invitedTo("secret-token")));

    expect(screen.getByRole("status")).toHaveTextContent(
      "This invitation has expired",
    );
    expect(
      screen.getByRole("heading", { name: "Sign in to garam" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });
});
