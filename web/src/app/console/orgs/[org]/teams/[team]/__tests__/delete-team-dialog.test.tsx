import { render, screen } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteTeamDialog } from "@/app/console/orgs/[org]/teams/[team]/_components/delete-team-dialog";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const GROUP = `${API_URL}/orgs/acme/groups/eng`;

/**
 * Shows what the dialog's action threw. A delete that succeeds ends in
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

function renderDialog() {
  return render(
    <Navigation>
      <DeleteTeamDialog
        org="acme"
        team="eng"
        teamName="Engineering"
        memberships="2 memberships"
      />
    </Navigation>,
  );
}

describe("delete team dialog", () => {
  it("names the team and what goes with it before deleting anything", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(GROUP, ({ request }) => {
        deleted.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByRole("button", { name: "Delete team" }));

    expect(
      screen.getByRole("alertdialog", { name: "Delete Engineering?" }),
    ).toHaveAccessibleDescription(
      "This removes the team and its 2 memberships. The people and agents in it stay in this organization. garam cannot bring the team back.",
    );
    // The act is named on the button that does it, not "Confirm".
    expect(screen.getByRole("button", { name: "Delete team" })).toHaveAttribute(
      "type",
      "submit",
    );
    expect(deleted).toEqual([]);
  });

  it("keeps the team and says why in the dialog when garam refuses", async () => {
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
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByRole("button", { name: "Delete team" }));
    await user.click(screen.getByRole("button", { name: "Delete team" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();

    // The control: the same dialog garam accepts returns to the list,
    // so staying above is garam's refusal and not the dialog.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Delete team" }));
    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs/acme/teams"),
    ).toBeInTheDocument();
  });
});
