import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteTeamForm } from "@/app/console/orgs/[org]/teams/[team]/_components/delete-team-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const GROUP = `${API_URL}/orgs/acme/groups/eng`;

function renderForm() {
  return render(
    <DeleteTeamForm
      org="acme"
      team="eng"
      teamName="Engineering"
      memberCount={2}
    />,
  );
}

describe("delete team form", () => {
  it("names the team and what goes with it before deleting anything", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(GROUP, ({ request }) => {
        deleted.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Delete team" }));

    // Presence: the sentence goes on to say what stays.
    expect(
      screen.getByText(
        /^Delete Engineering\? This removes the team and its 2 memberships;/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Delete Engineering" }),
    ).toBeInTheDocument();
    expect(deleted).toEqual([]);
  });

  it("keeps the team and shows why when garam refuses the delete", async () => {
    server.use(
      http.delete(GROUP, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Delete team" }));
    await user.click(
      screen.getByRole("button", { name: "Delete Engineering" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
  });
});
