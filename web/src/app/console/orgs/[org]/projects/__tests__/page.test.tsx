import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ProjectsPage from "@/app/console/orgs/[org]/projects/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
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
    await renderServerComponent(ProjectsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Projects" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Frontend")).toBeInTheDocument();
  });

  it("shows an empty state rather than a bare list", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([])),
    );

    await renderServerComponent(ProjectsPage(params("acme")));

    expect(
      screen.getByText("This organization has no projects."),
    ).toBeInTheDocument();
  });
});
