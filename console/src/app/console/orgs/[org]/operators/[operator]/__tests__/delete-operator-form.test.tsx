import { render, screen, within } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteOperatorForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/delete-operator-form";
import { enter } from "@/testing/enter";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// The real `unstable_rethrow` tells a redirect from a failed request,
// so the mocked one carries the digest a real redirect does.
vi.mock("next/navigation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("next/navigation")>()),
  redirect: (url: string) => {
    throw Object.assign(new Error(`NEXT_REDIRECT:${url}`), {
      digest: `NEXT_REDIRECT;push;${url};307;`,
    });
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// list it sends the reader back to is what reports the record is gone.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

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

/** Records the operators the form deleted, and answers as garam does. */
function recordDeletions() {
  const deleted: string[] = [];
  server.use(
    http.delete(`${API_URL}/orgs/acme/operators/:operator`, ({ request }) => {
      deleted.push(new URL(request.url).pathname);
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return deleted;
}

function renderForm() {
  return render(
    <Navigation>
      <DeleteOperatorForm
        org="acme"
        operator="field-one"
        name="Field cluster one"
      />
    </Navigation>,
  );
}

describe("delete operator form", () => {
  it("deletes once the identifier is typed, and returns to the list", async () => {
    const deleted = recordDeletions();
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Delete operator" }));
    const dialog = within(
      screen.getByRole("alertdialog", { name: "Delete Field cluster one?" }),
    );
    const confirm = dialog.getByRole("button", { name: "Delete operator" });
    expect(confirm).toBeDisabled();
    await enter(
      user,
      dialog.getByLabelText(/Type field-one to confirm/),
      "field-on",
    );
    expect(confirm).toBeDisabled();
    await user.clear(dialog.getByLabelText(/Type field-one to confirm/));
    await enter(
      user,
      dialog.getByLabelText(/Type field-one to confirm/),
      "field-one",
    );
    await user.click(confirm);

    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs/acme/operators"),
    ).toBeInTheDocument();
    expect(deleted).toEqual(["/orgs/acme/operators/field-one"]);
  });

  it("sends nothing when the dialog is cancelled", async () => {
    const deleted = recordDeletions();
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Delete operator" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(deleted).toEqual([]);
  });

  it("says what stops and that work in its cluster does not", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Delete operator" }));

    expect(screen.getByRole("alertdialog")).toHaveTextContent(
      "garam refuses its keys and credential; work in its cluster does not stop.",
    );
  });

  it("renders garam's refusal inside the dialog and keeps it open", async () => {
    server.use(
      http.delete(`${API_URL}/orgs/acme/operators/field-one`, () =>
        HttpResponse.json(
          {
            kind: "failed_precondition",
            message: "the operator is not deletable now",
          },
          { status: 409 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: "Delete operator" }));
    const dialog = within(screen.getByRole("alertdialog"));
    await enter(
      user,
      dialog.getByLabelText(/Type field-one to confirm/),
      "field-one",
    );
    await user.click(dialog.getByRole("button", { name: "Delete operator" }));

    expect(await dialog.findByRole("alert")).not.toBeEmptyDOMElement();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });
});
