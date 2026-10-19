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

// `refresh` is how an opened dialog reads the preview again; the test
// records each call.
const { refresh } = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  useRouter: () => ({ refresh }),
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

async function open(read: DeletionPreviewRead = OK, name = "Acme") {
  const user = userEvent.setup();
  render(
    <Navigation>
      <DeleteOrgDialog org="acme" name={name} read={read} />
    </Navigation>,
  );
  await user.click(screen.getByRole("button", { name: "Delete organization" }));
  const dialog = within(
    screen.getByRole("alertdialog", { name: `Delete ${name}?` }),
  );
  return { user, dialog };
}

/** Closes the dialog with Cancel and opens it again. */
async function reopen(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Delete organization" }));
  return within(screen.getByRole("alertdialog"));
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

  it("reads the preview again each time the dialog opens", async () => {
    refresh.mockClear();
    const user = userEvent.setup();
    render(<DeleteOrgDialog org="acme" name="Acme" read={OK} />);

    // Nothing is read again until the dialog is opened.
    expect(refresh).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "Delete organization" }),
    );
    expect(refresh).toHaveBeenCalledTimes(1);
    await reopen(user);
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it("says an organization holding nothing counted holds nothing else", async () => {
    const none = Object.fromEntries(
      Object.keys(acmeDeletionPreview.counts).map((kind) => [kind, 0]),
    ) as typeof acmeDeletionPreview.counts;
    const { dialog } = await open({
      status: "ok",
      preview: { certificateAuthority: false, counts: none },
    });

    expect(dialog.getByText("It holds nothing else yet.")).toBeInTheDocument();
    expect(dialog.queryAllByRole("listitem")).toEqual([]);
  });

  it("forgets the typed name and a refusal when the dialog closes", async () => {
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
    ).toBeInTheDocument();

    const reopened = await reopen(user);

    expect(reopened.getByLabelText(/Type Acme to confirm/)).toHaveValue("");
    expect(
      reopened.queryByText(/^You don't have permission to do this\./),
    ).toBeNull();
    expect(
      reopened.getByRole("button", { name: "Delete organization" }),
    ).toBeDisabled();
  });

  it("matches a name stored decomposed when it is typed composed", async () => {
    const stored = "가람".normalize("NFD");
    const { user, dialog } = await open(OK, stored);
    const field = dialog.getByLabelText(/to confirm/);
    const confirm = dialog.getByRole("button", { name: "Delete organization" });
    expect(stored).not.toBe("가람");

    // The control: a different name stays refused in the same dialog.
    await enter(user, field, "가랑");
    expect(confirm).toBeDisabled();
    await user.clear(field);
    await enter(user, field, "가람");

    expect(confirm).toBeEnabled();
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
