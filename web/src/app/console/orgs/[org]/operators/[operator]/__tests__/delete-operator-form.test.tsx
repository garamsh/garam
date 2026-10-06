import { render, screen } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteOperatorForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/delete-operator-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { setupUser } from "@/testing/user";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
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

function renderForm(enrolled: boolean) {
  return render(
    <Navigation>
      <DeleteOperatorForm
        org="acme"
        operator="field-one"
        name="Field cluster one"
        enrolled={enrolled}
      />
    </Navigation>,
  );
}

describe("delete operator form", () => {
  it("deletes on the confirmation and returns to the list", async () => {
    // The refusal and its control are read together: the first click
    // sends nothing, and the same recorder shows the confirmed one
    // sending — so what held the request back is the confirmation
    // rather than a form that never posts.
    const deleted = recordDeletions();
    const user = setupUser();
    renderForm(true);

    await user.click(screen.getByRole("button", { name: "Delete operator" }));

    expect(deleted).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Confirm deletion" }));

    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs/acme/operators"),
    ).toBeInTheDocument();
    expect(deleted).toEqual(["/orgs/acme/operators/field-one"]);
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const deleted = recordDeletions();
    const user = setupUser();
    renderForm(true);

    await user.click(screen.getByRole("button", { name: "Delete operator" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(deleted).toEqual([]);
    expect(
      screen.getByRole("button", { name: "Delete operator" }),
    ).toBeInTheDocument();
  });

  it("says what stops and that work in its cluster does not, when it enrolled", async () => {
    const user = setupUser();
    renderForm(true);

    await user.click(screen.getByRole("button", { name: "Delete operator" }));

    expect(
      screen.getByText(
        "Delete Field cluster one? Its new connections to garam are refused. Work already running in its cluster does not stop, and agents it provisioned keep their own certificates.",
      ),
    ).toBeInTheDocument();
  });

  it("says nothing stops when the operator never enrolled", async () => {
    const user = setupUser();
    renderForm(false);

    await user.click(screen.getByRole("button", { name: "Delete operator" }));

    expect(
      screen.getByText(
        "Delete Field cluster one? It never obtained a certificate, so nothing stops. Its identifier becomes free to register again.",
      ),
    ).toBeInTheDocument();
  });

  it("renders the refusal a naming definition draws, and stays on the page", async () => {
    let refusing = true;
    server.use(
      http.delete(`${API_URL}/orgs/acme/operators/field-one`, () =>
        refusing
          ? HttpResponse.json(
              {
                kind: "failed_precondition",
                message:
                  "an agent definition names this operator: point it at another operator or delete it first",
              },
              { status: 409 },
            )
          : new HttpResponse(null, { status: 204 }),
      ),
    );
    const user = setupUser();
    renderForm(true);

    await user.click(screen.getByRole("button", { name: "Delete operator" }));
    await user.click(screen.getByRole("button", { name: "Confirm deletion" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "an agent definition names this operator",
    );

    // The control: the same confirmation garam accepts returns to the
    // list, so staying above is garam's refusal and not the form.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Confirm deletion" }));
    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs/acme/operators"),
    ).toBeInTheDocument();
  });
});
