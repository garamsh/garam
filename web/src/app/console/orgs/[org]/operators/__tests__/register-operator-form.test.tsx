import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RegisterOperatorForm } from "@/app/console/orgs/[org]/operators/_components/register-operator-form";
import { API_URL, fieldOneRegistered } from "@/testing/mocks/handlers";
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
      return HttpResponse.json(fieldOneRegistered, { status: 201 });
    }),
  );
  return posted;
}

describe("register operator form", () => {
  it("submits nothing before hydration: the server render disables Register operator", () => {
    // A form submitted before React hydrates posts fields React never
    // held, so the button waits with the fields it submits.
    const server = document.createElement("div");
    server.innerHTML = renderToString(<RegisterOperatorForm org="acme" />);
    expect(
      server.querySelector('button[type="submit"]')?.hasAttribute("disabled"),
    ).toBe(true);

    // The control: rendered in the browser, the same button is enabled.
    render(<RegisterOperatorForm org="acme" />);
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeEnabled();
  });

  it("posts the identifier alone when no name is given, and shows the enrollment token once", async () => {
    const posted = recordRegistrations();
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.click(screen.getByRole("button", { name: "Register operator" }));

    expect(
      await screen.findByRole("heading", {
        name: `Registered ${fieldOneRegistered.operator.name}`,
      }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Enrollment token")).toHaveValue(
      fieldOneRegistered.enrollment.token,
    );
    // Presence rather than identity: the sentence is longer than the
    // claim under test, which is that the page says the token is gone
    // after this.
    expect(
      screen.getByText(/This is the only time this token is shown/),
    ).toBeInTheDocument();
    // No name is sent, so the API calls the operator by its
    // identifier (ADR-0074).
    expect(posted).toEqual([{ identifier: "field-one", description: "" }]);
  });

  it("posts the name and description typed, and links to the operator's page", async () => {
    const posted = recordRegistrations();
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.type(screen.getByLabelText("Name"), "  Field cluster one ");
    await user.type(screen.getByLabelText("Description"), "Batch agents.");
    await user.click(screen.getByRole("button", { name: "Register operator" }));

    expect(
      await screen.findByRole("link", { name: "Open operator" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(posted).toEqual([
      {
        identifier: "field-one",
        name: "Field cluster one",
        description: "Batch agents.",
      },
    ]);
  });

  it("refuses a name past 100 characters without a round trip", async () => {
    const posted = recordRegistrations();
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.type(screen.getByLabelText("Name"), "a".repeat(101));

    expect(
      screen.getByText(
        "An operator's name holds at most 100 characters; this one holds 101.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeDisabled();

    // The control: one character fewer is taken.
    await user.type(screen.getByLabelText("Name"), "{Backspace}");
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeEnabled();
    expect(posted).toEqual([]);
  });

  it("keeps only what has to be read before typing over the input", async () => {
    recordRegistrations();
    const user = userEvent.setup();
    render(<RegisterOperatorForm org="acme" />);

    // Presence: each is one clause of a longer sentence. What a person
    // needs before typing stays; what the operator does with the token
    // is beside the token, and what deleting a record does is on the
    // operator's page.
    expect(
      screen.getByText(/Registering records that this organization has/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/Deleting a record stops that operator/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/on garam's machine listener/),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/certificate authority/)).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("Identifier"), "field-one");
    await user.click(screen.getByRole("button", { name: "Register operator" }));

    // The control: how the operator enrols is beside the token that it
    // enrols with, so its absence above is a move rather than a
    // deletion. The recovery named beside it is a new token, which the
    // API mints (ADR-0067), and not a deletion.
    expect(
      await screen.findByText(/on garam's machine listener/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/ask an administrator to mint a new enrollment token/),
    ).toBeInTheDocument();
  });

  it("shows no token before a registration answers", async () => {
    recordRegistrations();
    render(<RegisterOperatorForm org="acme" />);

    // The control on the case above: the field the token is read out of
    // is absent until the answer that carries one arrives, so what put
    // it on screen was the registration.
    expect(screen.queryByLabelText("Enrollment token")).not.toBeInTheDocument();
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
