import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { PublishTemplateForm } from "@/app/console/orgs/[org]/agents/_components/publish-template-form";
import type { HostedOperator } from "@/features/orgs";
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

const hosted: HostedOperator = {
  grn: "grn:acme:default:operator:hosted",
  identifier: "hosted",
  name: "Hosted",
  endpoint: ENDPOINT,
  delegation: null,
  problem: null,
};

function renderForm() {
  return render(
    <PublishTemplateForm
      org="acme"
      orgGrn="grn:root:default:org:acme"
      hosted={hosted}
      from={null}
      onPublished={() => {}}
    />,
  );
}

describe("publish template form", () => {
  it("submits nothing before hydration: the server render disables Publish version", () => {
    // A form submitted before React hydrates posts fields React never
    // held, so the button waits with the fields it submits.
    const html = document.createElement("div");
    html.innerHTML = renderToString(
      <PublishTemplateForm
        org="acme"
        orgGrn="grn:root:default:org:acme"
        hosted={hosted}
        from={null}
        onPublished={() => {}}
      />,
    );
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
      ["Tools, one tool=pin per line", "search=v1"],
    ];
    for (const [label, value] of typed) {
      await enter(user, form.getByLabelText(label), value);
    }
    await user.click(form.getByRole("button", { name: "Publish version" }));

    expect(await form.findByRole("alert")).toHaveTextContent(
      "the model needs an embeddings endpoint with a base URL and a name",
    );
    // The refusal is the control service's, so the form reached it.
    expect(posted).toHaveLength(1);
    for (const [label, value] of typed) {
      expect(form.getByLabelText(label)).toHaveValue(value);
    }
  });
});
