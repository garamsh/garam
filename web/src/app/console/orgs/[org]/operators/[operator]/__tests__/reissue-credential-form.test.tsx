import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ReissueCredentialForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/reissue-credential-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

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
    // The refusal and its control are read together: the first click
    // sends nothing, and the same recorder shows the confirmed one
    // sending — so what held the request back is the confirmation.
    const posted = recordReissues();
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Re-issue credential" }),
    );
    expect(posted).toEqual([]);
    // Presence: the sentence goes on to say when the operator works
    // again; the claim under test is that the old credential stops.
    expect(
      screen.getByText(/every token it exchanged for it stop authenticating/),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Confirm re-issue" }));

    expect(await screen.findByLabelText("Credential")).toHaveValue(reissued);
    expect(posted).toEqual(["/orgs/acme/operators/field-one/credential"]);
    expect(
      screen.getByText(/This is the only time this credential is shown/),
    ).toBeInTheDocument();
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const posted = recordReissues();
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Re-issue credential" }),
    );
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(
      screen.getByRole("button", { name: "Re-issue credential" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Credential")).not.toBeInTheDocument();
    expect(posted).toEqual([]);
  });

  it("renders the backend's refusal beside the control", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators/:operator/credential`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Re-issue credential" }),
    );
    await user.click(screen.getByRole("button", { name: "Confirm re-issue" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
    expect(screen.queryByLabelText("Credential")).not.toBeInTheDocument();
  });
});
