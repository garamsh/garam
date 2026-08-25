import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RegisterOperatorForm } from "@/app/console/orgs/[org]/operators/_components/register-operator-form";
import { API_URL, fieldOne } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Records every registration the form posts, and answers as garam does. */
function recordRegistrations() {
  const posted: unknown[] = [];
  server.use(
    http.post(`${API_URL}/orgs/acme/operators`, async ({ request }) => {
      posted.push(await request.json());
      return HttpResponse.json(fieldOne, { status: 201 });
    }),
  );
  return posted;
}

describe("register operator form", () => {
  it("posts the identifier and reports the operator the organization now holds", async () => {
    const posted = recordRegistrations();
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.click(screen.getByRole("button", { name: "Register operator" }));

    expect(
      await screen.findByText(
        "Registered field-one. An agent definition may name it now.",
      ),
    ).toBeInTheDocument();
    expect(posted).toEqual([{ identifier: "field-one" }]);
  });

  it("refuses an identifier outside the character rule without a round trip", async () => {
    const posted = recordRegistrations();
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "Field-One");

    expect(
      screen.getByText(
        "An operator identifier holds only lowercase letters, digits and hyphens.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeDisabled();
    expect(posted).toEqual([]);

    // The control: the same field holding a lowercase identifier is
    // accepted, so what refuses is the character rule and not the form.
    await user.clear(screen.getByLabelText("Identifier"));
    await user.type(screen.getByLabelText("Identifier"), "field-one");
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeEnabled();
  });

  it("renders the backend's refusal rather than replacing the form", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "operator is already registered" },
          { status: 409 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.click(screen.getByRole("button", { name: "Register operator" }));

    expect(
      await screen.findByText("operator is already registered"),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Identifier")).toHaveValue("field-one");
  });

  it("keeps what was typed when the API never answers", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, () => HttpResponse.error()),
    );
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.click(screen.getByRole("button", { name: "Register operator" }));

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByLabelText("Identifier")).toHaveValue("field-one");
  });
});
