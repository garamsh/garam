import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { useState } from "react";

import {
  PublishTemplateForm,
  startingDraft,
} from "@/app/console/orgs/[org]/agents/_components/publish-template-form";
import type { DelegatedOperator } from "@/features/orgs";
import { enter } from "@/testing/enter";
import { server } from "@/testing/setup";

// The authority is minted by a Server Action; what the form does with
// the control service's answer is the subject here.
vi.mock("@/features/orgs/actions", () => ({
  mintOperationAuthority: async () => ({
    status: "ok",
    authority: "test-authority",
  }),
}));

const ENDPOINT = "https://control.example.test/garam";

const hosted: DelegatedOperator = {
  grn: "grn:acme:default:operator:hosted",
  identifier: "hosted",
  name: "Hosted",
  endpoint: ENDPOINT,
  delegation: {
    grantId: "grant-1",
    operatorGrn: "grn:acme:default:operator:hosted",
    controllers: [],
    operations: ["agent-template:publish"],
    expiresAt: "2099-01-01T00:00:00Z",
    grantedBy: "grn:acme:default:user:alice",
    grantedAt: "2026-10-01T00:00:00Z",
  },
  problem: null,
};

/** The form with its draft held as the page holds it. */
function Form() {
  const [values, setValues] = useState(() => startingDraft(null));
  return (
    <PublishTemplateForm
      org="acme"
      orgGrn="grn:root:default:org:acme"
      hosted={hosted}
      values={values}
      onValuesChange={setValues}
      onPublished={() => {}}
    />
  );
}

function renderForm() {
  return render(<Form />);
}

describe("publish template form", () => {
  it("submits nothing before hydration: the server render disables Publish version", () => {
    // A form submitted before React hydrates posts fields React never
    // held, so the button waits with the fields it submits.
    const html = document.createElement("div");
    html.innerHTML = renderToString(<Form />);
    expect(
      html.querySelector('button[type="submit"]')?.hasAttribute("disabled"),
    ).toBe(true);
  });

  it("keeps every field as typed when the control service refuses", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(
        `${ENDPOINT}/v1/orgs/acme/templates/minimax/versions`,
        async ({ request }) => {
          posted.push(await request.json());
          return HttpResponse.json(
            {
              kind: "embedding_required",
              message:
                "the model needs an embeddings endpoint with a base URL and a name",
            },
            { status: 400 },
          );
        },
      ),
    );
    const user = userEvent.setup();
    const form = within(renderForm().container);

    const typed: [string, string][] = [
      ["Template", "minimax"],
      ["Profile", "small"],
      ["Profile version", "1"],
      ["Model provider", "openai-compatible"],
      ["Model base URL", "https://api.minimax.io/v1"],
      ["Model name", "MiniMax-M2"],
      ["API key reference", "minimax/key"],
      ["Embedding API key reference", "minimax/embedding-key"],
      ["Ego", "A careful reader."],
      ["Tools", "search=v1"],
    ];
    for (const [label, value] of typed) {
      await enter(user, form.getByLabelText(label), value);
    }
    await user.click(form.getByRole("button", { name: "Publish version" }));

    // Identity: the console's sentence for the kind, so the control
    // service's own text cannot be what is shown.
    expect(await form.findByRole("alert")).toHaveTextContent(
      /^This model needs an embeddings endpoint\. Give the embedding's base URL and model name, then try again\.$/,
    );
    // The refusal is the control service's, so the form reached it.
    expect(posted).toHaveLength(1);
    for (const [label, value] of typed) {
      expect(form.getByLabelText(label)).toHaveValue(value);
    }
  });

  it("attaches what it can check to the field it is about, and sends nothing", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${ENDPOINT}/v1/orgs/acme/*`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json({ name: "x", version: 1 });
      }),
    );
    const user = userEvent.setup();
    const form = within(renderForm().container);

    await enter(user, form.getByLabelText("Profile version"), "one");
    await enter(user, form.getByLabelText("Tools"), "search");
    await user.click(form.getByRole("button", { name: "Publish version" }));

    // The form action sets the errors after the click resolves.
    await vi.waitFor(() =>
      expect(form.getByLabelText("Template")).toHaveAccessibleDescription(
        "Name the template.",
      ),
    );
    expect(form.getByLabelText("Profile version")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(form.getByLabelText("Tools")).toHaveAccessibleDescription(
      "One tool=pin per line. Write each tool as tool=pin.",
    );
    // The control: a field it found nothing wrong in carries no error.
    expect(form.getByLabelText("Model name")).not.toHaveAttribute(
      "aria-invalid",
    );
    expect(posted).toEqual([]);
  });
});
