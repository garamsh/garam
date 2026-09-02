import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OrgPage from "@/app/console/orgs/[org]/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, signedOut } from "@/testing/mocks/handlers";
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

describe("organization overview page", () => {
  it("counts what the organization holds and links to each section", async () => {
    const { container } = await renderServerComponent(OrgPage(params("acme")));

    expect(screen.getByRole("heading", { name: "Acme" })).toBeInTheDocument();
    // Presence: a card's name is its section label and the count
    // rendered under it, and what this asserts is where the card links.
    expect(screen.getByRole("link", { name: /Groups/ })).toHaveAttribute(
      "href",
      "/console/orgs/acme/groups",
    );
    // Presence, as above: the count follows the label.
    expect(screen.getByRole("link", { name: /Members/ })).toHaveAttribute(
      "href",
      "/console/orgs/acme/members",
    );
    expect(
      screen.getByRole("link", { name: "Projects 1" }),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("counts zero rather than leaving blank space", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/members`, () => HttpResponse.json([])),
    );

    await renderServerComponent(OrgPage(params("acme")));

    expect(screen.getByRole("link", { name: "Groups 0" })).toBeInTheDocument();
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

  // The overview reads four paths and the sections read one each, so a
  // denial has to answer the same way here as it does there (ADR-0011).
  it.each(["groups", "projects", "members"])(
    "renders not-found when the %s read is denied, as that section does",
    async (segment) => {
      server.use(
        http.get(`${API_URL}/orgs/acme/${segment}`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
        ),
      );

      await expect(
        renderServerComponent(OrgPage(params("acme"))),
      ).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the API is unreachable rather than counting what it cannot read", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(OrgPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    // Presence: a card's name is the label and its count, so matching
    // the label alone is what shows no card rendered at all.
    expect(
      screen.queryByRole("link", { name: /Groups/ }),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(OrgPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
