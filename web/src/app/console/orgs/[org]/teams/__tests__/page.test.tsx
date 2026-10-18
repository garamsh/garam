import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import TeamsPage from "@/app/console/orgs/[org]/teams/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  signedOut,
} from "@/testing/mocks/handlers";
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

    // Presence: the heading goes on to carry the count.
    const heading = screen.getByRole("heading", { level: 1, name: /^Teams/ });
    expect(within(heading).getByText("1")).toBeInTheDocument();
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

  it.each([403, 404])(
    "renders not-found when the organization answers %i",
    async (status) => {
      server.use(
        http.get(`${API_URL}/orgs/acme`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status }),
        ),
      );

      await expect(
        renderServerComponent(TeamsPage(params("acme"))),
      ).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the list is refused rather than showing no teams", async () => {
    // The control: answered, the list shows its team.
    const allowed = await renderServerComponent(TeamsPage(params("acme")));
    expect(screen.getByRole("link", { name: "Engineering" })).toBeVisible();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderServerComponent(TeamsPage(params("acme")));

    expect(screen.getByRole("status")).toHaveTextContent(
      // Presence: the notice goes on to say where access comes from.
      /^You can't view this organization's teams/,
    );
    expect(
      screen.queryByText("This organization has no teams."),
    ).not.toBeInTheDocument();
    // No count is guessed for a list that was not read.
    expect(
      screen.getByRole("heading", { level: 1, name: "Teams" }),
    ).toBeInTheDocument();
  });

  it("leaves out the create action only when the server denies it", async () => {
    server.use(answerPermissionChecks(() => "unknown"));
    const unknown = await renderServerComponent(TeamsPage(params("acme")));
    expect(screen.getByRole("link", { name: "Create team" })).toBeVisible();
    unknown.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "group:create" ? "deny" : "allow",
      ),
    );
    await renderServerComponent(TeamsPage(params("acme")));

    expect(
      screen.queryByRole("link", { name: "Create team" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Engineering" })).toBeVisible();
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
  it("leaves out the create action on a refused list only when denied", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );
    // The control: refused the list but allowed to create, the action
    // stays beside the notice.
    const allowed = await renderServerComponent(TeamsPage(params("acme")));
    expect(screen.getByRole("link", { name: "Create team" })).toBeVisible();
    allowed.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "group:create" ? "deny" : "allow",
      ),
    );
    await renderServerComponent(TeamsPage(params("acme")));

    expect(screen.getByRole("status")).toHaveTextContent(
      // Presence: the notice goes on to say where access comes from.
      /^You can't view this organization's teams/,
    );
    expect(
      screen.queryByRole("link", { name: "Create team" }),
    ).not.toBeInTheDocument();
  });
});
