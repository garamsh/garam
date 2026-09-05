import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ProjectsPage from "@/app/console/orgs/[org]/projects/page";
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

describe("projects section", () => {
  it("lists the organization's projects by name", async () => {
    const { container } = await renderServerComponent(
      ProjectsPage(params("acme")),
    );

    expect(
      screen.getByRole("heading", { name: "Projects" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Frontend")).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows an empty state rather than a bare list", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([])),
    );

    await renderServerComponent(ProjectsPage(params("acme")));

    expect(
      screen.getByText("This organization has no projects."),
    ).toBeInTheDocument();
    // The empty state names the absence and no next step; the next
    // step is the form standing under it, which is where every
    // section that creates in place puts one.
    expect(
      screen.getByRole("button", { name: "Create project" }),
    ).toBeInTheDocument();
  });

  it("offers the create form to anyone who may read the section", async () => {
    await renderServerComponent(ProjectsPage(params("acme")));

    expect(screen.getByLabelText("Project name")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create project" }),
    ).toBeInTheDocument();
  });

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/projects`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(ProjectsPage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

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
