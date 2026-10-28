import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import InvitationPage from "@/app/console/invitations/[token]/page";
import { API_URL, acmeInvitation } from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

const PREVIEW = `${API_URL}/invitations/secret-token`;

function renderPage() {
  return renderServerComponent(
    InvitationPage({ params: Promise.resolve({ token: "secret-token" }) }),
  );
}

describe("invitation page", () => {
  it("names the organization and the role, and offers to join", async () => {
    await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: "Join Acme" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "You've been invited to join Acme as an admin. Accepting adds you to the organization with that role.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Joining as an admin")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Join Acme" })).toBeEnabled();
  });

  it.each([
    [
      "accepted",
      "This invitation has already been used",
      "An invitation to Acme works once. If you haven't joined yet, ask an admin of Acme for a new one.",
    ],
    [
      "expired",
      "This invitation has expired",
      "Ask an admin of Acme to send you a new one.",
    ],
  ] as const)(
    "says an %s invitation is spent and offers no way to accept it",
    async (state, title, body) => {
      server.use(
        http.get(PREVIEW, () =>
          HttpResponse.json({ ...acmeInvitation, state }),
        ),
      );

      await renderPage();

      expect(
        screen.getByRole("heading", { level: 1, name: title }),
      ).toBeInTheDocument();
      expect(screen.getByText(body)).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /join/i })).toBeNull();
      expect(
        screen.getByRole("link", { name: "Go to your organizations" }),
      ).toHaveAttribute("href", "/console/orgs");
    },
  );

  it("says an unknown token is not a valid link", async () => {
    server.use(
      http.get(PREVIEW, () =>
        HttpResponse.json(
          { kind: "not_found", message: "invitation not found" },
          { status: 404 },
        ),
      ),
    );

    await renderPage();

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "This invitation link isn't valid",
      }),
    ).toBeInTheDocument();
    expect(screen.queryByText("invitation not found")).toBeNull();
    expect(screen.queryByRole("button", { name: /join/i })).toBeNull();
  });

  it("says the API did not answer rather than offering a button that cannot work", async () => {
    server.use(http.get(PREVIEW, () => HttpResponse.error()));

    await renderPage();

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /join/i })).toBeNull();
  });
});
