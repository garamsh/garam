import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RemoveTeamMemberForm } from "@/app/console/orgs/[org]/teams/[team]/_components/remove-team-member-form";
import { API_URL, alice } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
// The table the page revalidates is what shows the member gone.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const MEMBER = `${API_URL}/orgs/acme/groups/eng/members/:member`;

function renderForm() {
  return render(
    <RemoveTeamMemberForm
      org="acme"
      team="eng"
      teamName="Engineering"
      member={alice.grn}
      label="Alice"
    />,
  );
}

describe("remove team member form", () => {
  it("removes on the confirmation naming both, not on the first click", async () => {
    const removed: string[] = [];
    server.use(
      http.delete(MEMBER, ({ request }) => {
        removed.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    );
    // Presence: the confirmation is one sentence naming both sides.
    expect(
      screen.getByText(/^Remove Alice from Engineering\?/),
    ).toBeInTheDocument();
    expect(removed).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Confirm removal" }));

    await vi.waitFor(() =>
      expect(removed).toEqual([
        `/orgs/acme/groups/eng/members/${encodeURIComponent(alice.grn)}`,
      ]),
    );
  });

  it("backs out without removing anything", async () => {
    const removed: string[] = [];
    server.use(
      http.delete(MEMBER, ({ request }) => {
        removed.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    );
    // Cancel takes focus, so a stray Enter keeps the member.
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    await user.keyboard("{Enter}");

    expect(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    ).toBeInTheDocument();
    expect(removed).toEqual([]);
  });

  it("shows a failed removal beside the member", async () => {
    server.use(
      http.delete(MEMBER, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    );
    await user.click(screen.getByRole("button", { name: "Confirm removal" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
  });
});
