import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import RegisterPage from "@/app/(auth)/register/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, unauthenticated } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("register page", () => {
  it("asks for an invitation rather than offering an open form", async () => {
    server.use(unauthenticated());

    await renderServerComponent(RegisterPage());

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
    await expect(renderServerComponent(RegisterPage())).rejects.toThrow(
      "NEXT_REDIRECT:/console",
    );
  });

  it("says the API is unreachable instead of failing", async () => {
    server.use(http.get(`${API_URL}/me`, () => HttpResponse.error()));

    await renderServerComponent(RegisterPage());

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Invitation token")).not.toBeInTheDocument();
  });
});
