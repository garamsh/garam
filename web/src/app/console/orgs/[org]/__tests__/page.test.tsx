import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OrgPage from "@/app/console/orgs/[org]/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, fieldOne, signedOut } from "@/testing/mocks/handlers";
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
  it("names the organization and prints no GRN", async () => {
    const { container } = await renderServerComponent(OrgPage(params("acme")));

    expect(screen.getByRole("heading", { name: "Acme" })).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  // Presence: a card's accessible name is its label, its count and any
  // failing state under it, so the label alone cannot be identity.
  it.each([
    ["Teams", "/console/orgs/acme/teams"],
    ["Projects", "/console/orgs/acme/projects"],
    ["Operators", "/console/orgs/acme/operators"],
    ["Agents", "/console/orgs/acme/agents"],
    ["People", "/console/orgs/acme/people"],
  ])("summarises %s and links to it", async (label, href) => {
    await renderServerComponent(OrgPage(params("acme")));

    expect(
      screen.getByRole("link", { name: new RegExp(`^${label}\\b`) }),
    ).toHaveAttribute("href", href);
  });

  it("says why the two sections it does not summarise are absent", async () => {
    await renderServerComponent(OrgPage(params("acme")));

    // Presence: were either card rendered, its name would open with the
    // label and carry a count after it.
    expect(
      screen.queryByRole("link", { name: /^Consents\b/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: /^Permissions\b/ }),
    ).not.toBeInTheDocument();
    // Presence: the note is one sentence and each reason is a clause of
    // it, so neither reason is the whole string.
    const note = screen.getByText(/not counted here/);
    expect(note).toHaveTextContent("Consents and Permissions");
    expect(note).toHaveTextContent("requires the admin role");
  });

  it("names the failing states a count on its own hides", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          {
            ...fieldOne,
            grn: "grn:acme:default:operator:field-two",
            enrollmentState: "expired",
            certificateIssuedAt: undefined,
          },
        ]),
      ),
    );

    await renderServerComponent(OrgPage(params("acme")));

    // Presence, as above: the assertion is the state under the count
    // rather than the whole of the card's name.
    expect(
      screen.getByRole("link", { name: /^Operators\b/ }),
    ).toHaveTextContent("1 expired");
    // The agent states are counted apart because the operator and the
    // agent report separately (ADR-0053). The fixture's single agent is
    // in both counts, which one "not running" number would hide.
    const agents = screen.getByRole("link", { name: /^Agents\b/ });
    expect(agents).toHaveTextContent("1 failed");
    expect(agents).toHaveTextContent("1 disconnected");
  });

  it("puts no failing state on a section that has none", async () => {
    await renderServerComponent(OrgPage(params("acme")));

    // Identity: with nothing to act on, the whole of a card's name is
    // its label and its count, so any attention line breaks this. The
    // fixture's one operator is `enrolled` and its one agent is not.
    expect(
      screen.getByRole("link", { name: "Operators 1" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Teams 1" })).toBeInTheDocument();
  });

  it("counts zero rather than leaving blank space", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/projects`, () => HttpResponse.json([])),
      http.get(`${API_URL}/orgs/acme/members`, () => HttpResponse.json([])),
    );

    await renderServerComponent(OrgPage(params("acme")));

    expect(screen.getByRole("link", { name: "Teams 0" })).toBeInTheDocument();
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

  // The overview reads six paths and the sections read one each, so a
  // denial has to answer the same way here as it does there (ADR-0011).
  // Every one of them needs the `member` role, which is what the
  // overview itself needs to render at all.
  it.each(["groups", "projects", "operators", "agents", "members"])(
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
      screen.queryByRole("link", { name: /Teams/ }),
    ).not.toBeInTheDocument();
  });

  // Each read is one of the six, so the notice has to stand for
  // whichever of them did not answer rather than for the first alone.
  it.each(["operators", "agents"])(
    "says the API is unreachable when the %s read is the one that failed",
    async (segment) => {
      server.use(
        http.get(`${API_URL}/orgs/acme/${segment}`, () => HttpResponse.error()),
      );

      await renderServerComponent(OrgPage(params("acme")));

      expect(
        screen.getByRole("heading", { name: "Cannot reach the API" }),
      ).toBeInTheDocument();
    },
  );

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(OrgPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
