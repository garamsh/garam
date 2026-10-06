import { render, screen } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import { renderToString } from "react-dom/server";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DefineAgentForm } from "@/app/console/orgs/[org]/agents/_components/define-agent-form";
import { API_URL, fieldAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { setupUser } from "@/testing/user";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/**
 * Shows the error a render threw. In the app `redirect()` throws to
 * navigate and Next.js catches it; here this stands where Next.js
 * would, so the navigation is read rather than left unhandled.
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

type Operators = { identifier: string; name: string }[] | null;

/** Renders the form where Next.js would, starting from `values`. */
function renderForm(operators: Operators, values: Record<string, string> = {}) {
  return render(
    <Navigation>
      <DefineAgentForm org="acme" operators={operators} values={values} />
    </Navigation>,
  );
}

/** Where a created agent sends the person: its own page. */
const OPENED = "NEXT_REDIRECT:/console/orgs/acme/agents/9f2ac1b40d8e7a35";

/** Records every definition the form posts, and answers as garam does. */
function recordDefinitions() {
  const posted: unknown[] = [];
  server.use(
    http.post(`${API_URL}/orgs/acme/agent-definitions`, async ({ request }) => {
      posted.push(await request.json());
      return HttpResponse.json(fieldAgent, { status: 201 });
    }),
  );
  return posted;
}

describe("define agent form", () => {
  it("submits nothing before hydration: the server render disables Define agent", () => {
    // A form submitted before React hydrates posts fields React never
    // held, so the button waits with the fields it submits.
    const server = document.createElement("div");
    server.innerHTML = renderToString(
      <DefineAgentForm
        org="acme"
        operators={[{ identifier: "field-one", name: "Field one" }]}
        values={{}}
      />,
    );
    expect(
      server.querySelector('button[type="submit"]')?.hasAttribute("disabled"),
    ).toBe(true);

    // The control: rendered in the browser, the same button is enabled.
    renderForm([{ identifier: "field-one", name: "Field one" }]);
    expect(screen.getByRole("button", { name: "Create agent" })).toBeEnabled();
  });

  it("chooses the operator by name and posts the identifier beside it", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm([
      { identifier: "field-one", name: "Field cluster" },
      { identifier: "field-two", name: "Field cluster" },
      { identifier: "staging", name: "staging" },
    ]);

    // Two operators share a name, so each option carries the
    // identifier that tells them apart; one named by its identifier is
    // not written twice.
    const options = screen
      .getAllByRole("option")
      .map((option) => option.textContent);
    expect(options).toEqual([
      "Choose an operator",
      "Field cluster (field-one)",
      "Field cluster (field-two)",
      "staging",
    ]);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.selectOptions(
      screen.getByLabelText("Operator"),
      "Field cluster (field-two)",
    );
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toEqual([
      {
        name: "Scout",
        operatorGrn: "grn:acme:default:operator:field-two",
        values: {},
      },
    ]);
  });

  it("offers registering when there is no operator to choose", async () => {
    renderForm([]);

    expect(
      screen.getByRole("link", { name: "Register an operator" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators");
    // The control: with an operator to choose, the offer is gone, so
    // what put it there is the empty list.
    renderForm([{ identifier: "field-one", name: "Field one" }]);
    expect(
      screen.getAllByRole("link", { name: "Register an operator" }),
    ).toHaveLength(1);
  });

  it("renders the refusal an unregistered operator gets, and prints no GRN", async () => {
    // The API refuses a definition naming an operator the organization
    // has not registered (ADR-0045) and names the identifier rather
    // than the GRN, which is what lets the console render the sentence
    // it is given (ADR-0011).
    server.use(
      http.post(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json(
          {
            kind: "invalid_argument",
            message:
              "this organization has registered no operator named gagent-dev",
          },
          { status: 400 },
        ),
      ),
    );
    const user = setupUser();
    const { container } = renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "gagent-dev");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(
      await screen.findByText(
        "this organization has registered no operator named gagent-dev",
      ),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });
  it("builds the operator GRN from the organization and posts the values as a map", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "model");
    await user.type(screen.getByLabelText("Value 1"), "haiku");
    await user.click(screen.getByRole("button", { name: "Add value" }));
    await user.type(screen.getByLabelText("Key 2"), "workspace");
    await user.type(screen.getByLabelText("Value 2"), "frontend");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toEqual([
      {
        name: "Scout",
        operatorGrn: "grn:acme:default:operator:field-one",
        values: { model: "haiku", workspace: "frontend" },
      },
    ]);
  });

  it("defines an agent carrying no values at all", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toEqual([
      {
        name: "Scout",
        operatorGrn: "grn:acme:default:operator:field-one",
        values: {},
      },
    ]);
  });

  it("names no key of its own, and takes whichever key is typed", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    // The form names no key: garam interprets none, and the contract
    // that would give one meaning belongs to an operator garam has
    // never seen (ADR-0012). No suggestion offered, and no sample key
    // standing in the field either.
    expect(document.querySelectorAll("datalist option")).toHaveLength(0);
    expect(screen.getByLabelText("Key 1")).not.toHaveAttribute("placeholder");

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "cluster-region");
    await user.type(screen.getByLabelText("Value 1"), "eu-west-1");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toEqual([
      {
        name: "Scout",
        operatorGrn: "grn:acme:default:operator:field-one",
        values: { "cluster-region": "eu-west-1" },
      },
    ]);
  });

  it("says which key the character rule refuses, without a round trip", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);
    const define = screen.getByRole("button", { name: "Create agent" });

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "Model");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      'The key "Model" holds something other than lowercase letters, digits and hyphens.',
    );
    expect(define).toBeDisabled();
    expect(posted).toEqual([]);

    // The control: the same row with the key cased down is accepted, so
    // the character rule is what refused it.
    await user.clear(screen.getByLabelText("Key 1"));
    await user.type(screen.getByLabelText("Key 1"), "model");
    await user.click(define);

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toHaveLength(1);
  });

  it("refuses a repeated key, and takes the same two rows under distinct keys", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "model");
    await user.click(screen.getByRole("button", { name: "Add value" }));
    await user.type(screen.getByLabelText("Key 2"), "model");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      'The key "model" is used twice; a definition holds one value per key.',
    );
    expect(screen.getByRole("button", { name: "Create agent" })).toBeDisabled();

    await user.type(screen.getByLabelText("Key 2"), "-fallback");

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create agent" })).toBeEnabled();
    expect(posted).toEqual([]);
  });

  it("counts a value's bytes as it is typed and refuses one past the ceiling", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "system-prompt");
    await user.type(screen.getByLabelText("Value 1"), "be brief");

    // The ceiling is visible before it is reached, not only once it is.
    expect(screen.getByText("8 of 4096 bytes")).toBeInTheDocument();

    await user.clear(screen.getByLabelText("Value 1"));
    await user.click(screen.getByLabelText("Value 1"));
    await user.paste("x".repeat(4097));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      'The value under "system-prompt" is 4097 bytes; a value holds at most 4096.',
    );
    expect(screen.getByRole("button", { name: "Create agent" })).toBeDisabled();
    expect(posted).toEqual([]);
  });

  it("stops at thirty-two values and says that is all a definition carries", async () => {
    // Thirty-one values arrive the way a template's do, through the
    // form's own prop, so the test clicks once across the boundary
    // rather than thirty-one times to reach it.
    const user = setupUser();
    renderForm(
      null,
      Object.fromEntries(
        Array.from({ length: 31 }, (_, index) => [`key-${index}`, "v"]),
      ),
    );
    const add = screen.getByRole("button", { name: "Add value" });

    expect(screen.getByText("31 of 32 values")).toBeInTheDocument();
    expect(add).toBeEnabled();
    await user.click(add);

    expect(screen.getByText("32 of 32 values")).toBeInTheDocument();
    expect(add).toBeDisabled();

    // A further value is refused: the button takes no click, and the
    // editor still holds thirty-two rows.
    await user.click(add);
    expect(screen.getAllByRole("textbox", { name: /^Key / })).toHaveLength(32);
  });

  it("names the character rule an operator identifier breaks", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "acme:field");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "An operator identifier holds only lowercase letters, digits and hyphens.",
    );
    expect(screen.getByRole("button", { name: "Create agent" })).toBeDisabled();
    expect(posted).toEqual([]);
  });

  it("shows the backend's refusal", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/agent-definitions`, () =>
        HttpResponse.json(
          {
            kind: "invalid_argument",
            message: "agent: operator belongs to another organization",
          },
          { status: 400 },
        ),
      ),
    );
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "agent: operator belongs to another organization",
    );
  });

  it("keeps what was typed when the API does not answer", async () => {
    server.use(http.post(`${API_URL}/*`, () => HttpResponse.error()));
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "system-prompt");
    await user.type(screen.getByLabelText("Value 1"), "be brief");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "the garam API could not be reached",
    );
    expect(screen.getByLabelText("Value 1")).toHaveValue("be brief");
  });

  it("tells a person a credential is never a value", async () => {
    renderForm(null);

    // ADR-0036: garam holds the reference and no surface of its serves
    // the value, so the form must not invite one. Presence: the warning
    // closes a paragraph about what garam stores a value as.
    expect(
      screen.getByText(/Never put a credential in a value/),
    ).toBeInTheDocument();
  });
  it("starts from a template's values and posts them as edited", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null, { workspace: "papers", notes: "weekly" });

    // The copy is the form's own rows: shown as the template held
    // them, and edited here before anything is posted.
    expect(screen.getByLabelText("Key 1")).toHaveValue("workspace");
    expect(screen.getByLabelText("Value 2")).toHaveValue("weekly");
    await user.clear(screen.getByLabelText("Value 2"));
    await user.type(screen.getByLabelText("Value 2"), "daily");
    await user.type(screen.getByLabelText("Name"), "Scout");
    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toEqual([
      {
        name: "Scout",
        operatorGrn: "grn:acme:default:operator:field-one",
        values: { workspace: "papers", notes: "daily" },
      },
    ]);
  });

  it("refuses a name of only spaces, and posts the trimmed name", async () => {
    const posted = recordDefinitions();
    const user = setupUser();
    renderForm(null);

    await user.type(screen.getByLabelText("Name"), "   ");
    await user.type(screen.getByLabelText("Operator"), "field-one");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Give the agent a name.",
    );
    expect(screen.getByRole("button", { name: "Create agent" })).toBeDisabled();
    expect(posted).toEqual([]);

    // The control: the same name with a word inside it is posted,
    // trimmed as garam trims it.
    await user.type(screen.getByLabelText("Name"), "Scout ");
    await user.click(screen.getByRole("button", { name: "Create agent" }));

    expect(await screen.findByText(OPENED)).toBeInTheDocument();
    expect(posted).toEqual([
      {
        name: "Scout",
        operatorGrn: "grn:acme:default:operator:field-one",
        values: {},
      },
    ]);
  });
});
