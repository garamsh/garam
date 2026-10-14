import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { AcceptInvitationForm } from "@/app/console/invitations/[token]/_components/accept-invitation-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ACCEPT = `${API_URL}/invitations/secret-token/accept`;

function renderForm() {
  render(<AcceptInvitationForm token="secret-token" orgName="Acme" />);
}

describe("accept invitation form", () => {
  it("renders a refusal in words written for a person rather than replacing the page", async () => {
    server.use(
      http.post(ACCEPT, () =>
        HttpResponse.json(
          {
            kind: "failed_precondition",
            message: "invitation already accepted",
          },
          { status: 409 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Join Acme" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This invitation can no longer be used: it was already accepted or it has expired. Ask an admin of the organization for a new one.",
    );
    expect(screen.queryByText("invitation already accepted")).toBeNull();
  });

  it("keeps the page when the API never answers", async () => {
    server.use(http.post(ACCEPT, () => HttpResponse.error()));
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Join Acme" }));

    // No boundary covers this route, so a throw here would replace the
    // console with `global-error.tsx` — and the token is in the URL, so
    // the only retry path the visitor holds is this page.
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "garam can't be reached right now. Try again in a moment.",
    );
    expect(screen.getByRole("button", { name: "Join Acme" })).toBeEnabled();
  });
});
