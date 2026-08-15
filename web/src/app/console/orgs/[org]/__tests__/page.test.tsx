import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OrgPage from "@/app/console/orgs/[org]/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `notFound()` and `redirect()` throw control-flow errors the router
// catches; the tests only need to observe which one was reached.
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

describe("organization detail page", () => {
  it("renders the org with its groups, projects and members", async () => {
    await renderServerComponent(OrgPage(params("acme")));

    expect(screen.getByRole("heading", { name: "Acme" })).toBeInTheDocument();
    expect(screen.getByText("Engineering")).toBeInTheDocument();
    expect(screen.getByText("Frontend")).toBeInTheDocument();

    const memberRow = screen.getByRole("row", {
      name: /grn:root:default:user:alice/,
    });
    expect(memberRow).toHaveTextContent("owner");
    expect(memberRow).toHaveTextContent("2026-01-02");
  });

  it("reports empty sections rather than blank space", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/members`, () => HttpResponse.json([])),
    );

    await renderServerComponent(OrgPage(params("acme")));

    expect(
      screen.getByText("This organization has no groups."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("This organization has no projects."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("This organization has no members."),
    ).toBeInTheDocument();
  });

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(OrgPage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("offers invitations to an owner", async () => {
    await renderServerComponent(OrgPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Invite a member" }),
    ).toBeInTheDocument();
  });

  it("hides invitations from a plain member", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json([
          { userGrn: "grn:root:default:user:alice", role: "member", since: "2026-01-02T03:04:05Z" },
        ]),
      ),
    );

    await renderServerComponent(OrgPage(params("acme")));

    expect(
      screen.queryByRole("heading", { name: "Invite a member" }),
    ).not.toBeInTheDocument();
  });
});
