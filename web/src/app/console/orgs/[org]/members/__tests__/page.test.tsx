import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import MembersPage from "@/app/console/orgs/[org]/members/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, unauthenticated } from "@/testing/mocks/handlers";
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

describe("members section", () => {
  it("lists members by identifier rather than by GRN", async () => {
    const { container } = await renderServerComponent(
      MembersPage(params("acme")),
    );

    const row = screen.getByRole("row", { name: /alice/ });
    expect(row).toHaveTextContent("owner");
    expect(row).toHaveTextContent("2026-01-02");
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows an empty state rather than a bare table", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () => HttpResponse.json([])),
    );

    await renderServerComponent(MembersPage(params("acme")));

    expect(
      screen.getByText("This organization has no members."),
    ).toBeInTheDocument();
  });

  it("offers invitations to an owner", async () => {
    await renderServerComponent(MembersPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Invite a member" }),
    ).toBeInTheDocument();
  });

  it("hides invitations from a plain member", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json([
          {
            userGrn: "grn:root:default:user:alice",
            role: "member",
            since: "2026-01-02T03:04:05Z",
          },
        ]),
      ),
    );

    await renderServerComponent(MembersPage(params("acme")));

    expect(
      screen.queryByRole("heading", { name: "Invite a member" }),
    ).not.toBeInTheDocument();
  });

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(MembersPage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than reporting no members", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(MembersPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has no members."),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Invite a member" }),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(unauthenticated());

    await expect(
      renderServerComponent(MembersPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
