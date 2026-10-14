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

/** Picks `option` in the select labelled `label`, as a visitor does. */
async function choose(
  user: ReturnType<typeof userEvent.setup>,
  label: string,
  option: string,
) {
  await user.click(await screen.findByRole("combobox", { name: label }));
  await user.click(await screen.findByRole("option", { name: option }));
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

    await choose(user, "Template", "minimax version 1");
    await choose(user, "Controller", "Controller one");
    await user.click(
      screen.getByRole("button", { name: "Create from template" }),
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
    const user = userEvent.setup();
    renderStep();

    await user.click(
      await screen.findByRole("combobox", { name: "Controller" }),
    );

    expect(
      (await screen.findAllByRole("option")).map((o) => o.textContent),
    ).toEqual(["Controller one", "controller-two"]);
  });

  it("keeps the template and controller chosen when the control service refuses", async () => {
    const posted: unknown[] = [];
    server.use(
      publishedTemplates([
        { name: "minimax", version: 1, profile: { name: "small", version: 1 } },
        { name: "scout", version: 2, profile: { name: "small", version: 1 } },
      ]),
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
    await choose(user, "Template", "scout version 2");
    await choose(user, "Controller", "Controller two");
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
    // The form's reset after the action left both choices as they were.
    expect(
      screen.getByRole("combobox", { name: "Template" }),
    ).toHaveTextContent("scout version 2");
    expect(
      screen.getByRole("combobox", { name: "Controller" }),
    ).toHaveTextContent("Controller two");
  });

  it("flags only the choice left unmade, and sends nothing", async () => {
    const posted: unknown[] = [];
    server.use(
      publishedTemplates([
        { name: "minimax", version: 1, profile: { name: "small", version: 1 } },
      ]),
      http.post(`${ENDPOINT}/v1/orgs/acme/agents`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json({}, { status: 500 });
      }),
    );
    const user = userEvent.setup();
    renderStep();

    await choose(user, "Template", "minimax version 1");
    await user.click(
      screen.getByRole("button", { name: "Create from template" }),
    );

    const controller = screen.getByRole("combobox", { name: "Controller" });
    await vi.waitFor(() =>
      expect(controller).toHaveAttribute("aria-invalid", "true"),
    );
    expect(controller).toHaveAccessibleDescription(
      expect.stringContaining("Choose a controller."),
    );
    // The control: the template, which was chosen, is not flagged.
    expect(
      screen.getByRole("combobox", { name: "Template" }),
    ).not.toHaveAttribute("aria-invalid");
    expect(posted).toEqual([]);
  });

  it("says a delegation naming no controller is a block, rather than offering an empty choice", async () => {
    server.use(
      publishedTemplates([
        { name: "minimax", version: 1, profile: { name: "small", version: 1 } },
      ]),
    );
    render(
      <CreateFromTemplate
        org="acme"
        orgGrn="grn:root:default:org:acme"
        hosted={{
          ...hosted,
          delegation: { ...hosted.delegation, controllers: [] },
        }}
        controllers={{}}
        onCreated={() => {}}
      />,
    );

    expect(
      await screen.findByText(
        "Hosted's delegation names no controller, so there is no operator to place an agent on.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open the operator" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/hosted");
    expect(screen.queryByRole("combobox")).toBeNull();
  });
});
