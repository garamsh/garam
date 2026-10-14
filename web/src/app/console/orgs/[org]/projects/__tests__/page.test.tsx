import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ProjectsPage from "@/app/console/orgs/[org]/projects/page";
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

const refused = (status: number) =>
  HttpResponse.json({ kind: "permission_denied", message: "no" }, { status });

describe("projects section", () => {
  it("lists the organization's projects by name in a table, with the identifier to copy", async () => {
    const { container } = await renderServerComponent(
      ProjectsPage(params("acme")),
    );

    // The count sits in the heading beside the title, with no space in
    // its text, so the whole text is matched with the gap optional.
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      /^Projects\s*1$/,
    );
    const table = screen.getByRole("table", { name: "Projects" });
    const row = within(table).getByRole("row", { name: /^Frontend\b/ });
    expect(
      within(row).getByRole("button", {
        name: "Copy the identifier of Frontend: frontend",
      }),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows an empty state rather than a bare table", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([])),
    );

    await renderServerComponent(ProjectsPage(params("acme")));

    expect(
      screen.getByText("This organization has no projects."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    // The count sits in the heading beside the title, with no space in
    // its text, so the whole text is matched with the gap optional.
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      /^Projects\s*0$/,
    );
  });

  it.each(["allow", "unknown"] as const)(
    "offers the create action when the check answers %s",
    async (decision) => {
      server.use(answerPermissionChecks(() => decision));

      await renderServerComponent(ProjectsPage(params("acme")));

      expect(
        screen.getByRole("button", { name: "Create project" }),
      ).toBeInTheDocument();
    },
  );

  it("leaves the create action out when the server denies it, and keeps the list", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "project:create" ? "deny" : "allow",
      ),
    );

    await renderServerComponent(ProjectsPage(params("acme")));

    expect(
      screen.queryByRole("button", { name: "Create project" }),
    ).not.toBeInTheDocument();
    // The control: the same render still lists, so the absence is the
    // deny and not a page that failed to draw.
    expect(screen.getByRole("table", { name: "Projects" })).toBeInTheDocument();
  });

  it("says a refused list is not shown, rather than reporting none", async () => {
    server.use(http.get(`${API_URL}/orgs/acme/projects`, () => refused(403)));

    await renderServerComponent(ProjectsPage(params("acme")));

    expect(screen.getByText("Projects not shown")).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has no projects."),
    ).not.toBeInTheDocument();
    // No count is guessed for a list nobody read.
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      /^Projects$/,
    );
  });

  it.each([403, 404])(
    "renders not-found when the organization read answers %i",
    async (status) => {
      server.use(http.get(`${API_URL}/orgs/acme`, () => refused(status)));

      await expect(
        renderServerComponent(ProjectsPage(params("acme"))),
      ).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the API is unreachable rather than reporting no projects", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(ProjectsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has no projects."),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(ProjectsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
