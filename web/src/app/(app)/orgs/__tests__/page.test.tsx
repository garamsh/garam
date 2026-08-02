import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OrgsPage from "@/app/(app)/orgs/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, unauthenticated } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `redirect()` throws a control-flow error the router catches; the
// test only needs to observe where it was sent.
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

describe("organizations page", () => {
  it("lists the caller's organizations and links to each", async () => {
    await renderServerComponent(OrgsPage());

    expect(
      screen.getByRole("heading", { name: "Organizations" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Acme/ })).toHaveAttribute(
      "href",
      "/orgs/acme",
    );
  });

  it("says so when the caller belongs to none", async () => {
    server.use(http.get(`${API_URL}/orgs`, () => HttpResponse.json([])));

    await renderServerComponent(OrgsPage());

    expect(
      screen.getByText("You are not a member of any organization yet."),
    ).toBeInTheDocument();
    expect(screen.queryAllByRole("link")).toHaveLength(0);
  });

  it("sends a rejected session to the login page, not the error boundary", async () => {
    server.use(unauthenticated());

    await expect(renderServerComponent(OrgsPage())).rejects.toThrow(
      "NEXT_REDIRECT:/login",
    );
  });
});
