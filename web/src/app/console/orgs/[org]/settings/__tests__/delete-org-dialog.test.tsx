import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { Component, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import {
  DeleteOrgDialog,
  type DeletionPreviewRead,
} from "@/app/console/orgs/[org]/settings/_components/delete-org-dialog";
import { enter } from "@/testing/enter";
import { API_URL, acmeDeletionPreview } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Shows where the action's `redirect()` was sending the reader. */
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

const OK: DeletionPreviewRead = { status: "ok", preview: acmeDeletionPreview };

async function open(read: DeletionPreviewRead = OK) {
  const user = userEvent.setup();
  render(
    <Navigation>
      <DeleteOrgDialog org="acme" name="Acme" read={read} />
    </Navigation>,
  );
  await user.click(screen.getByRole("button", { name: "Delete organization" }));
  const dialog = within(
    screen.getByRole("alertdialog", { name: "Delete Acme?" }),
  );
  return { user, dialog };
}

describe("delete organization dialog", () => {
  it("states what garam counted the deletion takes, leaving out what it holds none of", async () => {
    const { dialog } = await open();

    const taken = dialog
      .getAllByRole("listitem")
      .map((item) => item.textContent);
    expect(taken).toEqual([
      "1membership",
      "1team",
      "1project",
      "1agent",
      "1agent definition",
      "1agent template",
      "1operator",
      "2policies",
      "1consent relation, which the other organization loses too",
    ]);
    expect(
      dialog.getByText("Its certificate authority goes too"),
    ).toBeInTheDocument();
  });

  it("says nothing of an authority the organization never had", async () => {
    const { dialog } = await open({
      status: "ok",
      preview: { ...acmeDeletionPreview, certificateAuthority: false },
    });

    expect(dialog.queryByText("Its certificate authority goes too")).toBeNull();
    // The control: the counts still render in the same dialog.
    expect(dialog.getByText("policies")).toBeInTheDocument();
  });

  it("deletes once the name is typed, and returns to the organizations", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(`${API_URL}/orgs/:org`, ({ request }) => {
        deleted.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, dialog } = await open();
    const confirm = dialog.getByRole("button", { name: "Delete organization" });
    const field = dialog.getByLabelText(/Type Acme to confirm/);

    expect(confirm).toBeDisabled();
    await enter(user, field, "acme");
    expect(confirm).toBeDisabled();
    await user.clear(field);
    await enter(user, field, "Acme");
    await user.click(confirm);

    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs"),
    ).toBeInTheDocument();
    expect(deleted).toEqual(["/orgs/acme"]);
  });

  it("renders a refusal in the dialog rather than throwing", async () => {
    server.use(
      http.delete(`${API_URL}/orgs/:org`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "org: denied" },
          { status: 403 },
        ),
      ),
    );
    const { user, dialog } = await open();
    await enter(user, dialog.getByLabelText(/Type Acme to confirm/), "Acme");
    await user.click(
      dialog.getByRole("button", { name: "Delete organization" }),
    );

    expect(
      await dialog.findByText(/^You don't have permission to do this\./),
    ).toHaveAttribute("role", "alert");
    expect(dialog.queryByText("org: denied")).toBeNull();
  });

  it.each(["forbidden", "unreachable"] as const)(
    "offers no delete when the preview is %s, since nothing says what it takes",
    async (status) => {
      const { dialog } = await open({ status });

      expect(
        dialog.getByText("What this deletes is not known"),
      ).toBeInTheDocument();
      expect(dialog.queryByLabelText(/to confirm/)).toBeNull();
      expect(
        dialog.getByRole("button", { name: "Delete organization" }),
      ).toBeDisabled();
    },
  );
});
