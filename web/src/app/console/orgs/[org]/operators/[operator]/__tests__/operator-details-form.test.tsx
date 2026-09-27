import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { OperatorDetailsForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/operator-details-form";
import { API_URL, fieldOne } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const OPERATOR = `${API_URL}/orgs/acme/operators/field-one`;

/** Records every patch the form sends, and answers as garam does. */
function recordPatches() {
  const patched: unknown[] = [];
  server.use(
    http.patch(OPERATOR, async ({ request }) => {
      const body = (await request.json()) as {
        name: string;
        description: string;
      };
      patched.push(body);
      return HttpResponse.json({ ...fieldOne, ...body });
    }),
  );
  return patched;
}

function renderForm() {
  return render(
    <OperatorDetailsForm
      org="acme"
      operator="field-one"
      name="Field cluster one"
      description="Runs the batch agents."
    />,
  );
}

describe("operator details form", () => {
  it("sends the name and description to the operator's own path", async () => {
    const patched = recordPatches();
    const user = userEvent.setup();
    renderForm();

    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), " Renamed ");
    await user.clear(screen.getByLabelText("Description"));
    await user.click(screen.getByRole("button", { name: "Save details" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Saved as Renamed.",
    );
    // An emptied description is sent, and clears the one there was.
    expect(patched).toEqual([{ name: "Renamed", description: "" }]);
  });

  it("refuses an empty name without a round trip", async () => {
    const patched = recordPatches();
    const user = userEvent.setup();
    renderForm();

    await user.clear(screen.getByLabelText("Name"));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Give the operator a name.",
    );
    expect(screen.getByRole("button", { name: "Save details" })).toBeDisabled();

    // The control: a name typed back is taken.
    await user.type(screen.getByLabelText("Name"), "Field");
    expect(screen.getByRole("button", { name: "Save details" })).toBeEnabled();
    expect(patched).toEqual([]);
  });

  it("renders garam's refusal and keeps what was typed", async () => {
    server.use(
      http.patch(OPERATOR, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "Renamed");
    await user.click(screen.getByRole("button", { name: "Save details" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Renamed");
  });
});
