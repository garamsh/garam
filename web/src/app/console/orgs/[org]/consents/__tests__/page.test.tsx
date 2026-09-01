import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ConsentsPage from "@/app/console/orgs/[org]/consents/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, globexConsent, signedOut } from "@/testing/mocks/handlers";
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

describe("consents section", () => {
  it("names the other organization and both halves of the pair", async () => {
    const { container } = await renderServerComponent(
      ConsentsPage(params("acme")),
    );

    expect(
      screen.getByRole("heading", { name: "Consents" }),
    ).toBeInTheDocument();
    expect(screen.getByText("globex")).toBeInTheDocument();
    expect(screen.getByText("Messages cross")).toBeInTheDocument();
    // Both halves stand, and each is named on its own: the relation is
    // a pair and not one organization's switch (ADR-0035).
    expect(screen.getAllByText("Granted")).toHaveLength(2);
    // ADR-0011: the console addresses a resource by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("says which half is missing when the other organization has not granted", async () => {
    serveConsents({
      ...globexConsent,
      grantedThere: false,
      stands: false,
    });

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.getByText(
        "This organization has granted its half. Messages stay refused until globex grants its own.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Not granted")).toBeInTheDocument();
  });

  it("says which half is missing when this organization has not granted", async () => {
    serveConsents({
      ...globexConsent,
      grantedHere: false,
      stands: false,
    });

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.getByText(
        "globex has granted its half. Messages stay refused until this organization grants its own.",
      ),
    ).toBeInTheDocument();
    // Withdrawing clears this organization's own half, and there is
    // none to clear.
    expect(
      screen.queryByRole("button", { name: "Withdraw" }),
    ).not.toBeInTheDocument();
  });

  it("marks a standing relation apart from a waiting one", async () => {
    serveConsents(globexConsent, {
      orgGrn: "grn:root:default:org:initech",
      grantedHere: true,
      grantedThere: false,
      stands: false,
    });

    await renderServerComponent(ConsentsPage(params("acme")));

    // The property is that the two are told apart without reading the
    // words, so what is asserted is that their markings differ — not
    // which one either takes.
    expect(screen.getByText("Messages cross").dataset.variant).not.toEqual(
      screen.getByText("Waiting").dataset.variant,
    );
  });

  it("offers the withdrawal only for the half this organization holds", async () => {
    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.getByRole("button", { name: "Withdraw" }),
    ).toBeInTheDocument();
  });

  it("says the organization holds no relation rather than leaving the area blank", async () => {
    serveConsents();

    await renderServerComponent(ConsentsPage(params("acme")));

    expect(
      screen.getByText(
        "This organization holds no relation with another, so a message between one of its agents and an agent outside it is refused back to the sender.",
      ),
    ).toBeInTheDocument();
  });

  it("offers the grant form beside the list", async () => {
    await renderServerComponent(ConsentsPage(params("acme")));

    expect(screen.getByLabelText("Other organization")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Grant consent" }),
    ).toBeInTheDocument();
  });

  it.each([403, 404])(
    "renders not-found on the read answering %i",
    async (status) => {
      server.use(
        http.get(`${API_URL}/orgs/acme/consents`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status }),
        ),
      );

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
    expect(
      screen.queryByText(
        "This organization holds no relation with another, so a message between one of its agents and an agent outside it is refused back to the sender.",
      ),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(ConsentsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
