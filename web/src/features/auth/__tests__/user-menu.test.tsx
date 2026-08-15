import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { UserMenu } from "@/features/auth/components/user-menu";
import { unauthenticated } from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

// `redirect()` throws a control-flow error the router catches; the
// test only needs to observe where it was sent.
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("user menu", () => {
  it("shows the signed-in user's nickname and a sign-out button", async () => {
    await renderServerComponent(UserMenu());

    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Sign out" }),
    ).toBeInTheDocument();
  });

  it("sends a rejected session to the login page", async () => {
    server.use(unauthenticated());

    await expect(renderServerComponent(UserMenu())).rejects.toThrow(
      "NEXT_REDIRECT:/login",
    );
  });
});
