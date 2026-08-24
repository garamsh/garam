import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DefineAgentForm } from "@/app/console/orgs/[org]/agents/_components/define-agent-form";
import { API_URL, fieldAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

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
  it("builds the operator GRN from the organization and posts the values as a map", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "model");
    await user.type(screen.getByLabelText("Value 1"), "haiku");
    await user.click(screen.getByRole("button", { name: "Add value" }));
    await user.type(screen.getByLabelText("Key 2"), "workspace");
    await user.type(screen.getByLabelText("Value 2"), "frontend");
    await user.click(screen.getByRole("button", { name: "Define agent" }));

    expect(await screen.findByLabelText("Agent identifier")).toHaveValue(
      "9f2ac1b40d8e7a35",
    );
    expect(posted).toEqual([
      {
        operatorGrn: "grn:acme:default:operator:field-one",
        values: { model: "haiku", workspace: "frontend" },
      },
    ]);
  });

  it("defines an agent carrying no values at all", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.click(screen.getByRole("button", { name: "Define agent" }));

    expect(
      await screen.findByLabelText("Agent identifier"),
    ).toBeInTheDocument();
    expect(posted).toEqual([
      {
        operatorGrn: "grn:acme:default:operator:field-one",
        values: {},
      },
    ]);
  });

  it("accepts a key nobody suggested, so the suggestions are not the set", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    // The suggestions the key field offers, neither of which is used
    // here: they are a starting point, not the set of keys accepted.
    const suggested = Array.from(
      document.querySelectorAll<HTMLOptionElement>("datalist option"),
    ).map((option) => option.value);
    expect(suggested).toEqual(["model", "system-prompt"]);

    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "cluster-region");
    await user.type(screen.getByLabelText("Value 1"), "eu-west-1");
    await user.click(screen.getByRole("button", { name: "Define agent" }));

    expect(
      await screen.findByLabelText("Agent identifier"),
    ).toBeInTheDocument();
    expect(posted).toEqual([
      {
        operatorGrn: "grn:acme:default:operator:field-one",
        values: { "cluster-region": "eu-west-1" },
      },
    ]);
  });

  it("says which key the character rule refuses, without a round trip", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);
    const define = screen.getByRole("button", { name: "Define agent" });

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

    expect(
      await screen.findByLabelText("Agent identifier"),
    ).toBeInTheDocument();
    expect(posted).toHaveLength(1);
  });

  it("refuses a repeated key, and takes the same two rows under distinct keys", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "model");
    await user.click(screen.getByRole("button", { name: "Add value" }));
    await user.type(screen.getByLabelText("Key 2"), "model");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      'The key "model" is used twice; a definition holds one value per key.',
    );
    expect(screen.getByRole("button", { name: "Define agent" })).toBeDisabled();

    await user.type(screen.getByLabelText("Key 2"), "-fallback");

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Define agent" })).toBeEnabled();
    expect(posted).toEqual([]);
  });

  it("counts a value's bytes as it is typed and refuses one past the ceiling", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

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
    expect(screen.getByRole("button", { name: "Define agent" })).toBeDisabled();
    expect(posted).toEqual([]);
  });

  it("stops at thirty-two values and says that is all a definition carries", async () => {
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);
    const add = screen.getByRole("button", { name: "Add value" });

    expect(screen.getByText("1 of 32 values")).toBeInTheDocument();
    for (let added = 0; added < 31; added += 1) {
      await user.click(add);
    }

    expect(
      screen.getByText("32 of 32 values — a definition carries no more."),
    ).toBeInTheDocument();
    expect(add).toBeDisabled();
  });

  it("names the character rule an operator identifier breaks", async () => {
    const posted = recordDefinitions();
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    await user.type(screen.getByLabelText("Operator"), "acme:field");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "An operator identifier holds only lowercase letters, digits and hyphens.",
    );
    expect(screen.getByRole("button", { name: "Define agent" })).toBeDisabled();
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
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.click(screen.getByRole("button", { name: "Define agent" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "agent: operator belongs to another organization",
    );
  });

  it("keeps what was typed when the API does not answer", async () => {
    server.use(http.post(`${API_URL}/*`, () => HttpResponse.error()));
    const user = userEvent.setup();
    render(<DefineAgentForm org="acme" />);

    await user.type(screen.getByLabelText("Operator"), "field-one");
    await user.type(screen.getByLabelText("Key 1"), "system-prompt");
    await user.type(screen.getByLabelText("Value 1"), "be brief");
    await user.click(screen.getByRole("button", { name: "Define agent" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "the garam API could not be reached",
    );
    expect(screen.getByLabelText("Value 1")).toHaveValue("be brief");
  });

  it("tells a person a credential is never a value", async () => {
    render(<DefineAgentForm org="acme" />);

    // ADR-0036: garam holds the reference and no surface of its serves
    // the value, so the form must not invite one.
    expect(
      screen.getByText(/Never put a credential in a value/),
    ).toBeInTheDocument();
  });
});
