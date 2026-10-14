import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RemoveTeamMemberDialog } from "@/app/console/orgs/[org]/teams/[team]/_components/remove-team-member-dialog";
import { API_URL, alice } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const MEMBER = `${API_URL}/orgs/acme/groups/eng/members/:member`;

function renderDialog() {
  return render(
    <RemoveTeamMemberDialog
      org="acme"
      team="eng"
      teamName="Engineering"
      member={alice.grn}
      label="Alice"
    />,
  );
}

describe("remove team member dialog", () => {
  it("removes on the dialog naming both, not on the first click", async () => {
    const removed: string[] = [];
    server.use(
      http.delete(MEMBER, ({ request }) => {
        removed.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderDialog();

    await user.click(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    );
    expect(
      screen.getByRole("alertdialog", {
        name: "Remove Alice from Engineering?",
      }),
    ).toHaveAccessibleDescription(
      "Only this team membership goes. Nothing else changes in the organization or in other teams.",
    );
    expect(removed).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Remove from team" }));

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
    renderDialog();

    await user.click(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    );
    // Cancel takes focus, so a stray Enter keeps the member.
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    await user.keyboard("{Enter}");

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(removed).toEqual([]);
  });

  it("keeps a refusal in the dialog that asked", async () => {
    let refusing = true;
    const removed: string[] = [];
    server.use(
      http.delete(MEMBER, ({ request }) => {
        if (refusing) {
          return HttpResponse.json(
            { kind: "permission_denied", message: "permission denied" },
            { status: 403 },
          );
        }
        removed.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    renderDialog();

    await user.click(
      screen.getByRole("button", { name: "Remove Alice from Engineering" }),
    );
    await user.click(screen.getByRole("button", { name: "Remove from team" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();

    // The control: the same dialog garam accepts removes the member and
    // closes.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Remove from team" }));
    // The dialog stays until the action answers, so its going is the
    // answer rendered; the request is recorded before that.
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(removed).toHaveLength(1);
  });
});
