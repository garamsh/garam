import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import TeamsPage from "@/app/console/orgs/[org]/teams/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, signedOut } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function params(org: string) {
  return { params: Promise.resolve({ org }) };
}

describe("teams section", () => {
  it("lists the organization's teams, each linking to its own page", async () => {
    const { container } = await renderServerComponent(
      TeamsPage(params("acme")),
    );

    expect(screen.getByRole("heading", { name: "Teams" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Engineering" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/teams/eng",
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("offers team creation as a page of its own", async () => {
    await renderServerComponent(TeamsPage(params("acme")));

    expect(screen.getByRole("link", { name: "Create team" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/teams/new",
    );
  });

  it("shows an empty state rather than a bare list", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () => HttpResponse.json([])),
    );

    await renderServerComponent(TeamsPage(params("acme")));

    expect(
      screen.getByText("This organization has no teams."),
    ).toBeInTheDocument();
    // The empty state names the absence and no next step; the next
    // step is the create action the header carries.
    expect(
      screen.getByRole("link", { name: "Create team" }),
    ).toBeInTheDocument();
  });

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(TeamsPage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than reporting no teams", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(TeamsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has no teams."),
    ).not.toBeInTheDocument();
  });

  it("says the API is unreachable when a dependency garam needs is down", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () =>
        HttpResponse.json(
          { kind: "unavailable", message: "internal error" },
          { status: 503 },
        ),
      ),
    );

    await renderServerComponent(TeamsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(TeamsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
