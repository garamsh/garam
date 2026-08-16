import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import LoginPage from "@/app/(auth)/login/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  signedOutWith,
  unauthenticated,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("login page", () => {
  afterEach(() => vi.restoreAllMocks());

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

  it("offers dev sign-in when the backend advertises it", async () => {
    server.use(...signedOutWith(["dev", "google"]));

    await renderServerComponent(LoginPage());

    expect(screen.getByRole("link", { name: "Dev sign-in" })).toHaveAttribute(
      "href",
      "/api/auth/login/dev",
    );
  });

  it("hides dev sign-in when the backend does not advertise it", async () => {
    server.use(...signedOutWith(["google"]));

    await renderServerComponent(LoginPage());

    expect(
      screen.queryByRole("link", { name: "Dev sign-in" }),
    ).not.toBeInTheDocument();
  });

  it("degrades to Google sign-in when the provider lookup fails", async () => {
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});
    server.use(
      http.get(`${API_URL}/auth/providers`, () =>
        HttpResponse.json(
          { kind: "not_found", message: "unknown route" },
          { status: 404 },
        ),
      ),
      unauthenticated(),
    );

    await renderServerComponent(LoginPage());

    expect(
      screen.getByRole("link", { name: "Sign in with Google" }),
    ).toHaveAttribute("href", "/api/auth/login/google");
    expect(
      screen.queryByRole("link", { name: "Dev sign-in" }),
    ).not.toBeInTheDocument();
    expect(logged).toHaveBeenCalled();
  });

  it("sends an already signed-in visitor to the console", async () => {
    await expect(renderServerComponent(LoginPage())).rejects.toThrow(
      "NEXT_REDIRECT:/console",
    );
  });
});
