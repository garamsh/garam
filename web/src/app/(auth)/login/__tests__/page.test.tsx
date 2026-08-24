import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import LoginPage from "@/app/(auth)/login/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, unauthenticated } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("login page", () => {
  it("offers the Google sign-in button while signed out", async () => {
    server.use(unauthenticated());

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
    server.use(unauthenticated());

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

  it("sends a visitor holding an invitation to the registration page", async () => {
    server.use(unauthenticated());

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

  it("sends an already signed-in visitor to the console", async () => {
    await expect(renderServerComponent(LoginPage())).rejects.toThrow(
      "NEXT_REDIRECT:/console",
    );
  });
});
