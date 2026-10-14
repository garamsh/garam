import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ConsentsPage from "@/app/console/orgs/[org]/consents/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  globexConsent,
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

/** Answers the consents read with the relations the test is about. */
function serveConsents(...consents: unknown[]) {
  server.use(
    http.get(`${API_URL}/orgs/acme/consents`, () =>
      HttpResponse.json(consents),
    ),
  );
}

const refused = (status: number) =>
  HttpResponse.json({ kind: "permission_denied", message: "no" }, { status });

/** The card for the pair with `other`. */
function card(other: string) {
  return within(screen.getByRole("group", { name: `Consent with ${other}` }));
}

describe("consents section", () => {
  it("names the other organization, where the pair stands, and both halves", async () => {
    const { container } = await renderServerComponent(
      ConsentsPage(params("acme")),
    );

    const globex = card("globex");
    expect(globex.getByText("globex")).toBeInTheDocument();
    expect(globex.getByText("Messages cross")).toBeInTheDocument();
    // Both halves stand, and each is named on its own: the relation is
    // a pair and not one organization's switch (ADR-0035).
    expect(globex.getAllByText("Granted")).toHaveLength(2);
    // ADR-0011: the console addresses a resource by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("grants from the card of a pair waiting on this organization", async () => {
    serveConsents({ ...globexConsent, grantedHere: false, stands: false });

    await renderServerComponent(ConsentsPage(params("acme")));

    const globex = card("globex");
    expect(
      globex.getByText("Waiting for this organization"),
    ).toBeInTheDocument();
    expect(
      globex.getByText(
        "globex has granted its half. Messages stay refused until this organization grants its own.",
      ),
    ).toBeInTheDocument();
    expect(
      globex.getByRole("button", { name: "Grant consent" }),
    ).toBeInTheDocument();
    // Nothing of this organization's stands, so there is nothing of its
    // own to withdraw.
    expect(
      globex.queryByRole("button", { name: "Withdraw consent" }),
    ).not.toBeInTheDocument();
  });

  it("offers only the withdrawal on a pair waiting on the other organization", async () => {
    serveConsents({ ...globexConsent, grantedThere: false, stands: false });

    await renderServerComponent(ConsentsPage(params("acme")));

    const globex = card("globex");
    expect(globex.getByText("Waiting for globex")).toBeInTheDocument();
    expect(
      globex.getByText(
        "This organization has granted its half. Messages stay refused until globex grants its own.",
      ),
    ).toBeInTheDocument();
    expect(
      globex.getByRole("button", { name: "Withdraw consent" }),
    ).toBeInTheDocument();
    expect(
      globex.queryByRole("button", { name: "Grant consent" }),
    ).not.toBeInTheDocument();
  });

  it("leaves out each control the server denies, and only those", async () => {
    serveConsents(
      { ...globexConsent, grantedHere: false, stands: false },
      {
        orgGrn: "grn:root:default:org:initech",
        grantedHere: true,
        grantedThere: true,
        stands: true,
      },
    );
    server.use(
      answerPermissionChecks((action) =>
        action === "consent:grant" ? "deny" : "allow",
      ),
    );

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.queryByRole("button", { name: "New consent" }),
    ).not.toBeInTheDocument();
    expect(
      card("globex").queryByRole("button", { name: "Grant consent" }),
    ).not.toBeInTheDocument();
    // The control: the withdrawal the server allows is still offered.
    expect(
      card("initech").getByRole("button", { name: "Withdraw consent" }),
    ).toBeInTheDocument();
  });

  it("renders every control when the check could not be answered", async () => {
    serveConsents({ ...globexConsent, grantedHere: false, stands: false });
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.error(),
      ),
    );

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.getByRole("button", { name: "New consent" }),
    ).toBeInTheDocument();
    expect(
      card("globex").getByRole("button", { name: "Grant consent" }),
    ).toBeInTheDocument();
  });

  it("shows an empty state when the organization holds no relation", async () => {
    serveConsents();

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(screen.getByText("No consent relations")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "New consent" }),
    ).toBeInTheDocument();
  });

  it("says a refused list is not shown, rather than reporting none", async () => {
    server.use(http.get(`${API_URL}/orgs/acme/consents`, () => refused(403)));

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(screen.getByText("Consents not shown")).toBeInTheDocument();
    expect(screen.queryByText("No consent relations")).not.toBeInTheDocument();
  });

  it.each([403, 404])(
    "renders not-found when the organization read answers %i",
    async (status) => {
      server.use(http.get(`${API_URL}/orgs/acme`, () => refused(status)));

      await expect(
        renderServerComponent(ConsentsPage(params("acme"))),
      ).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the API is unreachable rather than reporting no relations", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No consent relations")).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(ConsentsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
