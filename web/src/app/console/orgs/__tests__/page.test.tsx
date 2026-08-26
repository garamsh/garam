import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OrgsPage from "@/app/console/orgs/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, signedOut } from "@/testing/mocks/handlers";
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
      "/console/orgs/acme",
    );
    expect(
      screen.getByRole("link", { name: "New organization" }),
    ).toHaveAttribute("href", "/console/orgs/new");
  });

  it("names organizations without their GRN", async () => {
    const { container } = await renderServerComponent(OrgsPage());

    // The control for the search: the listing rendered, so what is
    // absent is the GRN and not the page.
    expect(screen.getByRole("link", { name: /Acme/ })).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows an empty state when the caller belongs to none", async () => {
    server.use(http.get(`${API_URL}/orgs`, () => HttpResponse.json([])));

    await renderServerComponent(OrgsPage());

    expect(screen.getByText("No organizations yet")).toBeInTheDocument();
    // Only the "New organization" link remains.
    expect(screen.queryAllByRole("link")).toHaveLength(1);
  });

  it("says the API is unreachable rather than reporting no organizations", async () => {
    server.use(http.get(`${API_URL}/orgs`, () => HttpResponse.error()));

    await renderServerComponent(OrgsPage());

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No organizations yet")).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not the error boundary", async () => {
    server.use(...signedOut());

    await expect(renderServerComponent(OrgsPage())).rejects.toThrow(
      "NEXT_REDIRECT:/login",
    );
  });
});
