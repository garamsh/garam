import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ReissueCredentialForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/reissue-credential-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const reissued =
  "garam_oc_c81e728d9d4c2f636f067f89cc14862cc4ca4238a0b923820dcc509a6f75849b";

/** Records the operators the form re-issued for, and answers as garam does. */
function recordReissues() {
  const posted: string[] = [];
  server.use(
    http.post(
      `${API_URL}/orgs/acme/operators/:operator/credential`,
      ({ request }) => {
        posted.push(new URL(request.url).pathname);
        return HttpResponse.json(
          { credential: reissued, issuedAt: "2026-10-09T12:00:00Z" },
          { status: 201 },
        );
      },
    ),
  );
  return posted;
}

function renderForm() {
  return render(
    <ReissueCredentialForm org="acme" operator="field-one" name="Field one" />,
  );
}

describe("re-issue credential form", () => {
  it("re-issues on the confirmation and shows the new credential once", async () => {
    // The refusal and its control are read together: opening the dialog
    // sends nothing, and the same recorder shows the confirmed one.
    const posted = recordReissues();
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Re-issue credential" }),
    );
    const dialog = screen.getByRole("alertdialog", {
      name: "Re-issue Field one's credential?",
    });
    expect(dialog).toHaveTextContent(
      "The current credential and its tokens stop authenticating.",
    );
    expect(posted).toEqual([]);

    await user.click(
      within(dialog).getByRole("button", { name: "Re-issue credential" }),
    );

    expect(
      await screen.findByRole("alertdialog", {
        name: "Save the new credential",
      }),
    ).toHaveTextContent(reissued);
    expect(posted).toEqual(["/orgs/acme/operators/field-one/credential"]);
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const posted = recordReissues();
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Re-issue credential" }),
    );
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(posted).toEqual([]);
  });

  it("keeps the confirmation open with the backend's refusal in human words", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators/field-one/credential`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "denied: operator:reissue" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Re-issue credential" }),
    );
    const dialog = screen.getByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Re-issue credential" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(dialog).toBeInTheDocument();
    expect(screen.queryByText(/operator:reissue/)).not.toBeInTheDocument();
  });
});
