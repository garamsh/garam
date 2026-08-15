import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import InvitationPage from "@/app/console/invitations/[token]/page";
import { renderServerComponent } from "@/testing/render";

describe("accept invitation page", () => {
  it("renders the accept form for the token", async () => {
    await renderServerComponent(
      InvitationPage({ params: Promise.resolve({ token: "secret-token" }) }),
    );

    expect(
      screen.getByRole("heading", { name: "Join the organization" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Accept invitation" }),
    ).toBeInTheDocument();
  });
});
