import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import type { DelegatedOperator } from "@/app/console/orgs/[org]/agents/_components/control-notice";
import { OperatorTemplates } from "@/app/console/orgs/[org]/agents/_components/operator-templates";
import { server } from "@/testing/setup";

// The authority is minted by a Server Action; what the panel does with
// the control service's answers is the subject here.
vi.mock("@/features/orgs/actions", () => ({
  mintOperationAuthority: async () => ({
    status: "ok",
    authority: "test-authority",
  }),
}));

const ENDPOINT = "https://control.example.test/garam";
const BASE = `${ENDPOINT}/v1/orgs/acme`;

function hosted(operations: string[]): DelegatedOperator {
  return {
    grn: "grn:acme:default:operator:hosted",
    identifier: "hosted",
    name: "Hosted",
    endpoint: ENDPOINT,
    delegation: {
      grantId: "grant-1",
      operatorGrn: "grn:acme:default:operator:hosted",
      controllers: [],
      operations: operations as DelegatedOperator["delegation"]["operations"],
      expiresAt: "2099-01-01T00:00:00Z",
      grantedBy: "grn:acme:default:user:alice",
      grantedAt: "2026-10-01T00:00:00Z",
    },
    problem: null,
  };
}

const READS = [
  "agent-template:read",
  "agent-template:publish",
  "execution-profile:read",
];

function controlService() {
  server.use(
    http.get(`${BASE}/templates`, () =>
      HttpResponse.json({
        templates: [
          {
            name: "web agent",
            version: 1,
            profile: { name: "small", version: 1 },
          },
        ],
      }),
    ),
    http.get(`${BASE}/templates/web%20agent/versions/1`, () =>
      HttpResponse.json({
        name: "web agent",
        version: 1,
        profile: { name: "small", version: 1 },
        configuration: {
          model: {
            provider: "openai-compatible",
            baseUrl: "https://api.example.test/v1",
            name: "MiniMax-M2",
            apiKeyRef: "model-keys/web",
          },
          ego: "A careful reader.",
          tools: { search: "v1" },
        },
      }),
    ),
    http.get(`${BASE}/profiles`, () =>
      HttpResponse.json({ profiles: [{ name: "small", version: 1 }] }),
    ),
  );
}

function renderPanel(operations = READS, publishDenied = false) {
  return render(
    <OperatorTemplates
      org="acme"
      orgGrn="grn:root:default:org:acme"
      hosted={hosted(operations)}
      publishDenied={publishDenied}
    />,
  );
}

describe("control service templates", () => {
  it("lists what was published and shows a version's configuration, its secret by reference", async () => {
    controlService();
    const user = userEvent.setup();
    renderPanel();

    const table = await screen.findByRole("table", {
      name: "Published templates",
    });
    const row = within(table).getByRole("row", { name: /web agent/ });
    expect(within(row).getByText("small version 1")).toBeInTheDocument();

    await user.click(
      within(row).getByRole("button", { name: "View web agent" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: "web agent version 1",
    });
    expect(
      await within(dialog).findByText("reference model-keys/web"),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("search")).toBeInTheDocument();
  });

  it("offers publishing unless the server denies it or the grant lacks it", async () => {
    controlService();
    // The control: granted and not denied, publishing is offered.
    const offered = renderPanel();
    expect(
      await screen.findByRole("button", { name: "Publish version" }),
    ).toBeInTheDocument();
    offered.unmount();

    const denied = renderPanel(READS, true);
    await screen.findByRole("table", { name: "Published templates" });
    expect(
      screen.queryByRole("button", { name: "Publish version" }),
    ).toBeNull();
    denied.unmount();

    renderPanel(["agent-template:read", "execution-profile:read"]);
    await screen.findByRole("table", { name: "Published templates" });
    expect(
      screen.queryByRole("button", { name: "Publish version" }),
    ).toBeNull();
    expect(
      screen.getByText(
        "Hosted's delegation does not include “Publish template versions”.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Open the operator" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/hosted");
  });

  it("publishes from the dialog and says so beside Publish", async () => {
    controlService();
    const posted: unknown[] = [];
    server.use(
      http.post(
        `${BASE}/templates/web%20agent/versions`,
        async ({ request }) => {
          posted.push(await request.json());
          return HttpResponse.json({ name: "web agent", version: 2 });
        },
      ),
    );
    const user = userEvent.setup();
    renderPanel();

    await user.click(
      await screen.findByRole("button", { name: "View web agent" }),
    );
    await user.click(
      await screen.findByRole("button", {
        name: "Start a new version from this",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Publish a template version",
    });
    // Started from the version on screen.
    expect(within(dialog).getByLabelText("Ego")).toHaveValue(
      "A careful reader.",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Publish version" }),
    );

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Published web agent version 2.",
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(posted).toHaveLength(1);
  });

  it("keeps a typed draft when the publish dialog is closed and opened again", async () => {
    controlService();
    const user = userEvent.setup();
    renderPanel();

    await user.click(
      await screen.findByRole("button", { name: "Publish version" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Publish a template version",
    });
    expect(within(dialog).getByLabelText("Template")).toHaveValue("");
    await user.type(within(dialog).getByLabelText("Template"), "drafted");
    await user.keyboard("{Escape}");
    await vi.waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await user.click(screen.getByRole("button", { name: "Publish version" }));

    // The control: the field was empty before it was typed into, so
    // what reopened is the draft and not a default.
    expect(
      within(
        await screen.findByRole("dialog", {
          name: "Publish a template version",
        }),
      ).getByLabelText("Template"),
    ).toHaveValue("drafted");
  });
});
