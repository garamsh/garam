import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateFromTemplate } from "@/app/console/orgs/[org]/agents/_components/create-from-template";
import type { DelegatedOperator } from "@/app/console/orgs/[org]/agents/_components/control-notice";
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
const FIRST = "grn:acme:default:operator:controller-one";
const SECOND = "grn:acme:default:operator:controller-two";

const hosted: DelegatedOperator = {
  grn: "grn:acme:default:operator:hosted",
  identifier: "hosted",
  name: "Hosted",
  endpoint: ENDPOINT,
  delegation: {
    grantId: "grant-1",
    operatorGrn: "grn:acme:default:operator:hosted",
    controllers: [FIRST, SECOND],
    operations: ["agent:create", "agent-template:read"],
    expiresAt: "2099-01-01T00:00:00Z",
    grantedBy: "grn:acme:default:user:alice",
    grantedAt: "2026-10-01T00:00:00Z",
  },
  problem: null,
};

/** Answers the control service's template list with `templates`. */
function publishedTemplates(templates: unknown[]) {
  return http.get(`${ENDPOINT}/v1/orgs/acme/templates`, () =>
    HttpResponse.json({ templates }),
  );
}

function renderStep(onCreated: (created: unknown) => void = () => {}) {
  return render(
    <CreateFromTemplate
      org="acme"
      orgGrn="grn:root:default:org:acme"
      hosted={hosted}
      controllers={{ [FIRST]: "Controller one" }}
      onCreated={onCreated}
    />,
  );
}

describe("create from template", () => {
  it("hands the agent the control service created to the next step", async () => {
    const created: unknown[] = [];
    server.use(
      publishedTemplates([
        { name: "minimax", version: 1, profile: { name: "small", version: 1 } },
      ]),
      http.post(`${ENDPOINT}/v1/orgs/acme/agents`, () =>
        HttpResponse.json({
          agent: "grn:acme:default:agent:0123456789abcdef",
          revision: "1",
          epoch: "1",
        }),
      ),
    );
    const user = userEvent.setup();
    renderStep((agent) => created.push(agent));

    await user.click(
      await screen.findByRole("button", { name: "Create from template" }),
    );

    await vi.waitFor(() =>
      expect(created).toEqual([
        expect.objectContaining({
          agent: "grn:acme:default:agent:0123456789abcdef",
          revision: "1",
        }),
      ]),
    );
  });

  it("is no dead end when nothing is published: it links to Templates", async () => {
    server.use(publishedTemplates([]));
    renderStep();

    expect(
      await screen.findByText("No template is published yet"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open templates" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/templates");
    expect(
      screen.queryByRole("button", { name: "Create from template" }),
    ).toBeNull();
  });

  it("names a controller the organization has not named by its identifier, never its GRN", async () => {
    server.use(
      publishedTemplates([
        { name: "minimax", version: 1, profile: { name: "small", version: 1 } },
      ]),
    );
    render(
      <CreateFromTemplate
        org="acme"
        orgGrn="grn:root:default:org:acme"
        hosted={hosted}
        controllers={{ [FIRST]: "Controller one" }}
        onCreated={() => {}}
      />,
    );

    const controller = await screen.findByLabelText("Controller");
    expect(
      Array.from((controller as HTMLSelectElement).options, (o) => o.text),
    ).toEqual(["Controller one", "controller-two"]);
  });

  it("keeps the template and controller chosen when the control service refuses", async () => {
    const posted: unknown[] = [];
    server.use(
      http.get(`${ENDPOINT}/v1/orgs/acme/templates`, () =>
        HttpResponse.json({
          templates: [
            {
              name: "minimax",
              version: 1,
              profile: { name: "small", version: 1 },
            },
            {
              name: "scout",
              version: 2,
              profile: { name: "small", version: 1 },
            },
          ],
        }),
      ),
      http.post(`${ENDPOINT}/v1/orgs/acme/agents`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(
          { message: "the controller is at capacity" },
          { status: 409 },
        );
      }),
    );
    const user = userEvent.setup();
    render(
      <CreateFromTemplate
        org="acme"
        orgGrn="grn:root:default:org:acme"
        hosted={hosted}
        controllers={{ [FIRST]: "Controller one", [SECOND]: "Controller two" }}
        onCreated={() => {}}
      />,
    );

    // The second of each, so a reset to the first option shows.
    await user.selectOptions(await screen.findByLabelText("Template"), "scout");
    await user.selectOptions(screen.getByLabelText("Controller"), SECOND);
    await user.click(
      screen.getByRole("button", { name: "Create from template" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "the controller is at capacity",
    );
    // What was sent is what was chosen, so the refusal answered it.
    expect(posted).toEqual([
      expect.objectContaining({
        controller: SECOND,
        template: expect.objectContaining({ name: "scout" }),
      }),
    ]);
    expect(screen.getByLabelText("Template")).toHaveValue("scout");
    expect(screen.getByLabelText("Controller")).toHaveValue(SECOND);
  });
});
