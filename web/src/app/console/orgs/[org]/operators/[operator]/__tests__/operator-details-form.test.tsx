import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { OperatorDetailsForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/operator-details-form";
import { enter } from "@/testing/enter";
import { API_URL, fieldOne } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

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
  it("submits nothing before hydration: the server render disables Save details", () => {
    // A form submitted before React hydrates posts fields React never
    // held, so the button waits with the fields it submits.
    const server = document.createElement("div");
    server.innerHTML = renderToString(
      <OperatorDetailsForm
        org="acme"
        operator="field-one"
        name="Field cluster one"
        description=""
      />,
    );
    expect(
      server.querySelector('button[type="submit"]')?.hasAttribute("disabled"),
    ).toBe(true);

    // The control: rendered in the browser, the same button is enabled.
    render(
      <OperatorDetailsForm
        org="acme"
        operator="field-one"
        name="Field cluster one"
        description=""
      />,
    );
    expect(screen.getByRole("button", { name: "Save details" })).toBeEnabled();
  });

  it("sends the name and description to the operator's own path", async () => {
    const patched = recordPatches();
    const user = userEvent.setup();
    const form = within(renderForm().container);

    await user.clear(form.getByLabelText("Name"));
    await enter(user, form.getByLabelText("Name"), " Renamed ");
    await user.clear(form.getByLabelText("Description"));
    await user.click(form.getByRole("button", { name: "Save details" }));

    expect(await form.findByRole("status")).toHaveTextContent(
      "Saved as Renamed.",
    );
    // An emptied description is sent, and clears the one there was.
    expect(patched).toEqual([{ name: "Renamed", description: "" }]);
  });

  it("refuses an empty name without a round trip", async () => {
    const patched = recordPatches();
    const user = userEvent.setup();
    const form = within(renderForm().container);

    await user.clear(form.getByLabelText("Name"));

    expect(form.getByRole("alert")).toHaveTextContent(
      "Give the operator a name.",
    );
    expect(form.getByRole("button", { name: "Save details" })).toBeDisabled();

    // The control: a name typed back is taken.
    await enter(user, form.getByLabelText("Name"), "Field");
    expect(form.getByRole("button", { name: "Save details" })).toBeEnabled();
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
    const form = within(renderForm().container);

    await user.clear(form.getByLabelText("Name"));
    await enter(user, form.getByLabelText("Name"), "Renamed");
    await user.click(form.getByRole("button", { name: "Save details" }));

    expect(await form.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(form.getByLabelText("Name")).toHaveValue("Renamed");
  });
});
