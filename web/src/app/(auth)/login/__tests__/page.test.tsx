import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import LoginPage from "@/app/(auth)/login/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, signedOutWith } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("login page", () => {
  it("offers the Google sign-in button while signed out", async () => {
    server.use(...signedOutWith(["google"]));

    await renderServerComponent(LoginPage());

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

    await renderServerComponent(LoginPage());

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
    const registered = await renderServerComponent(LoginPage());
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
    await renderServerComponent(LoginPage());
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
    const known = await renderServerComponent(LoginPage());
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toBeInTheDocument();
    known.unmount();

    server.use(...signedOutWith(["github"]));
    await renderServerComponent(LoginPage());
    expect(
      screen.queryByRole("link", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("offers no provider button when the provider list cannot be read", async () => {
    server.use(...signedOutWith(["google"]));
    const readable = await renderServerComponent(LoginPage());
    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toBeInTheDocument();
    readable.unmount();

    server.use(
      ...signedOutWith([]),
      http.get(`${API_URL}/auth/providers`, () => HttpResponse.error()),
    );
    await renderServerComponent(LoginPage());
    expect(
      screen.queryByRole("link", { name: "Sign in with Google" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("sends a visitor holding an invitation to the registration page", async () => {
    server.use(...signedOutWith(["google"]));

    await renderServerComponent(LoginPage());

    expect(screen.getByRole("link", { name: "Create one" })).toHaveAttribute(
      "href",
      "/register",
    );
  });

  it("says the API is unreachable instead of failing", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await renderServerComponent(LoginPage());

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

    await renderServerComponent(LoginPage());

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Sign in" }),
    ).not.toBeInTheDocument();
  });

  it("sends an already signed-in visitor to the console", async () => {
    await expect(renderServerComponent(LoginPage())).rejects.toThrow(
      "NEXT_REDIRECT:/console",
    );
  });
});
