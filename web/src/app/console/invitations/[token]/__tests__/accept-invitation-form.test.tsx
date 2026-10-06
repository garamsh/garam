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

describe("accept invitation form", () => {
  it("renders the backend's refusal rather than replacing the page", async () => {
    server.use(
      http.post(ACCEPT, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "invitation already accepted" },
          { status: 409 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<AcceptInvitationForm token="secret-token" />);

    await user.click(screen.getByRole("button", { name: "Accept invitation" }));

    expect(
      await screen.findByText("invitation already accepted"),
    ).toBeInTheDocument();
  });

  it("keeps the page when the API never answers", async () => {
    server.use(http.post(ACCEPT, () => HttpResponse.error()));
    const user = userEvent.setup();
    render(<AcceptInvitationForm token="secret-token" />);

    await user.click(screen.getByRole("button", { name: "Accept invitation" }));

    // No boundary covers this route, so a throw here would replace the
    // console with `global-error.tsx` — and the token is in the URL, so
    // the only retry path the visitor holds is this page.
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Accept invitation" }),
    ).toBeInTheDocument();
  });
});
