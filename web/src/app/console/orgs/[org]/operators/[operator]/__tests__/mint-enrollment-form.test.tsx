import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { MintEnrollmentForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/mint-enrollment-form";
import { TooltipProvider } from "@/components/ui/tooltip";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const token = "8f14e45fceea167a5a36dedd4bea2543a1ba1ffc2fbd07a1";

/** Records the operators a token was minted for, answering as garam. */
function recordMints(
  answer: () => Response = () =>
    HttpResponse.json(
      { token, expiresAt: "2026-10-19T09:10:00Z" },
      { status: 201 },
    ),
) {
  const posted: string[] = [];
  server.use(
    http.post(
      `${API_URL}/orgs/acme/operators/:operator/enrollment`,
      ({ request }) => {
        posted.push(new URL(request.url).pathname);
        return answer();
      },
    ),
  );
  return posted;
}

function renderForm(blocked: string | null = null) {
  // The reveal names when the token expires, which carries a tooltip.
  return render(
    <TooltipProvider>
      <MintEnrollmentForm
        org="acme"
        operator="field-one"
        name="Field one"
        blocked={blocked}
      />
    </TooltipProvider>,
  );
}

const LABEL = "Issue a new enrollment token";

describe("enrollment token form", () => {
  it("mints on the confirmation and shows the token once", async () => {
    const posted = recordMints();
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: LABEL }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Issue Field one a new enrollment token?",
    });
    // Control: opening the dialog sends nothing.
    expect(posted).toEqual([]);

    await user.click(
      within(dialog).getByRole("button", { name: "Issue token" }),
    );

    const reveal = await screen.findByRole("alertdialog", {
      name: "Save the enrollment token",
    });
    expect(reveal).toHaveTextContent(token);
    expect(posted).toEqual(["/orgs/acme/operators/field-one/enrollment"]);

    await user.click(within(reveal).getByRole("checkbox", { name: /stored/ }));
    await user.click(within(reveal).getByRole("button", { name: "Done" }));

    // Shown once: closed, it is gone from the page and not offered back.
    expect(screen.queryByText(token)).not.toBeInTheDocument();
  });

  it("says inside the dialog why garam refused an operator that has enrolled", async () => {
    recordMints(() =>
      HttpResponse.json(
        {
          kind: "failed_precondition",
          message: "this operator has already enrolled",
        },
        { status: 409 },
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: LABEL }));
    const dialog = screen.getByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Issue token" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.operatorAlreadyEnrolled,
    );
    expect(
      screen.queryByRole("alertdialog", { name: "Save the enrollment token" }),
    ).not.toBeInTheDocument();
  });

  it("says a permission refusal in the shared words", async () => {
    recordMints(() =>
      HttpResponse.json(
        { kind: "permission_denied", message: "no" },
        { status: 403 },
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(screen.getByRole("button", { name: LABEL }));
    const dialog = screen.getByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Issue token" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
  });

  it("stays on the page, inactive and focusable, with the reason it cannot be used", async () => {
    const posted = recordMints();
    const user = userEvent.setup();
    renderForm("It has enrolled, so garam issues it no token.");

    const button = screen.getByRole("button", { name: LABEL });
    expect(button).toHaveAttribute("aria-disabled", "true");
    expect(button).toHaveAccessibleDescription(
      "It has enrolled, so garam issues it no token.",
    );
    await user.tab();
    expect(button).toHaveFocus();

    await user.click(button);

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(posted).toEqual([]);
  });
});
