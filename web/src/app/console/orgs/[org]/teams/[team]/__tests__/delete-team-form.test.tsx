import { render, screen } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteTeamForm } from "@/app/console/orgs/[org]/teams/[team]/_components/delete-team-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { setupUser } from "@/testing/user";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const GROUP = `${API_URL}/orgs/acme/groups/eng`;

/**
 * Shows what the form's action threw. A delete that succeeds ends in
 * `redirect()`, which throws to navigate; this is where it lands.
 */
class Navigation extends Component<
  { children: ReactNode },
  { to: string | null }
> {
  state = { to: null };
  static getDerivedStateFromError(error: Error) {
    return { to: error.message };
  }
  render() {
    return this.state.to ?? this.props.children;
  }
}

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
    const user = setupUser();
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
    let refusing = true;
    server.use(
      http.delete(GROUP, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : new HttpResponse(null, { status: 204 }),
      ),
    );
    const user = setupUser();
    render(
      <Navigation>
        <DeleteTeamForm
          org="acme"
          team="eng"
          teamName="Engineering"
          memberCount={2}
        />
      </Navigation>,
    );

    await user.click(screen.getByRole("button", { name: "Delete team" }));
    await user.click(
      screen.getByRole("button", { name: "Delete Engineering" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );

    // The control: the same confirmation garam accepts returns to the
    // list, so staying above is garam's refusal and not the form.
    refusing = false;
    await user.click(
      screen.getByRole("button", { name: "Delete Engineering" }),
    );
    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs/acme/teams"),
    ).toBeInTheDocument();
  });
});
