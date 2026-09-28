import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AgentPage from "@/app/console/orgs/[org]/agents/[agent]/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  alice,
  fieldAgent,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
}));

const ONE = `${API_URL}/orgs/acme/agent-definitions/9f2ac1b40d8e7a35`;
const params = {
  params: Promise.resolve({ org: "acme", agent: "9f2ac1b40d8e7a35" }),
};

describe("agent page", () => {
  it("names the agent, keeps what was requested apart from what was reported, and awaits an operator", async () => {
    const { container } = await renderServerComponent(AgentPage(params));

    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Identifier 9f2ac1b40d8e7a35")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Reported" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Awaiting operator")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Requested" }),
    ).toBeInTheDocument();
    expect(screen.getByDisplayValue("haiku")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(
      screen.getByRole("link", { name: "Manage policies" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=agent%3A9f2ac1b40d8e7a35",
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("shows the states an operator and the agent reported once one claims it", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([{ ...reportedAgent, grn: fieldAgent.agentGrn }]),
      ),
    );

    await renderServerComponent(AgentPage(params));

    expect(screen.getByText("failed")).toBeInTheDocument();
    expect(screen.getByText("disconnected")).toBeInTheDocument();
    expect(screen.queryByText("Awaiting operator")).not.toBeInTheDocument();
  });

  it("renames the agent through its own form", async () => {
    let body: unknown;
    server.use(
      http.patch(ONE, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ ...fieldAgent, name: "Ranger" });
      }),
    );
    const user = userEvent.setup();
    await renderServerComponent(AgentPage(params));

    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), " Ranger ");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Renamed to Ranger.",
    );
    expect(body).toEqual({ name: "Ranger" });
  });

  it("says a refused definition is refused rather than missing", async () => {
    let refusing = false;
    server.use(
      http.get(ONE, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "no" },
              { status: 403 },
            )
          : HttpResponse.json(fieldAgent),
      ),
    );
    // The control: answered, the same agent renders under its name and
    // says nothing is refused.
    const allowed = await renderServerComponent(AgentPage(params));
    expect(
      screen.getByRole("heading", { level: 1, name: "Scout" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    allowed.unmount();

    refusing = true;
    await renderServerComponent(AgentPage(params));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "garam refused to show this agent to you.",
    );
  });
  it("keeps a ready agent's settings unknown until its operator reports a revision", async () => {
    const ready = {
      ...reportedAgent,
      grn: fieldAgent.agentGrn,
      provisioningState: "ready" as const,
      runtimeState: "connected" as const,
      assignment: {
        operatorGrn: fieldAgent.operatorGrn,
        epoch: 1,
        since: "2026-01-02T03:04:05Z",
      },
    };
    let acknowledged = false;
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          acknowledged
            ? {
                ...ready,
                configuration: {
                  epoch: 1,
                  revision: 1,
                  outcome: "applied",
                  ignoredKeys: [],
                  reportedAt: "2026-01-04T00:00:00Z",
                },
              }
            : ready,
        ]),
      ),
    );

    const unreported = await renderServerComponent(AgentPage(params));
    expect(screen.getByText("Unknown")).toBeInTheDocument();
    expect(
      screen.getByText("No operator has reported applying these settings."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Applied")).not.toBeInTheDocument();
    unreported.unmount();

    // The control: the same agent acknowledging revision 1 is applied.
    acknowledged = true;
    await renderServerComponent(AgentPage(params));
    expect(screen.getByText("Applied")).toBeInTheDocument();
    expect(screen.queryByText("Unknown")).not.toBeInTheDocument();
  });

  it.each<[string, Record<string, unknown>, string, string]>([
    [
      "an older revision as pending",
      { revision: 1, outcome: "applied", ignoredKeys: [] },
      "Pending",
      "The operator last reported revision 1.",
    ],
    [
      "ignored keys as partly applied",
      { revision: 2, outcome: "applied", ignoredKeys: ["model"] },
      "Partly applied",
      "The operator reports ignoring model.",
    ],
    [
      "a rejection with its reason",
      { revision: 2, outcome: "rejected", reason: "image pull failed" },
      "Rejected",
      "The operator reports: image pull failed",
    ],
    [
      "a replaced operator's report as unknown",
      { epoch: 1, revision: 2, outcome: "applied", ignoredKeys: [] },
      "Unknown",
      "The last report came from a replaced operator.",
    ],
  ])("says %s", async (_case, report, label, detail) => {
    server.use(
      http.get(ONE, () => HttpResponse.json({ ...fieldAgent, revision: 2 })),
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            grn: fieldAgent.agentGrn,
            assignment: {
              operatorGrn: fieldAgent.operatorGrn,
              epoch: 2,
              since: "2026-01-02T03:04:05Z",
            },
            configuration: {
              epoch: 2,
              reportedAt: "2026-01-04T00:00:00Z",
              ...report,
            },
          },
        ]),
      ),
    );

    await renderServerComponent(AgentPage(params));

    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.getByText(detail)).toBeInTheDocument();
    expect(screen.queryByText("Applied")).not.toBeInTheDocument();
  });

  it("says the settings status is not checked when reports are refused", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderServerComponent(AgentPage(params));

    expect(screen.getByText("Not checked")).toBeInTheDocument();
    expect(screen.queryByText("Unknown")).not.toBeInTheDocument();
  });

  it("shows no health report time an operator never reported", async () => {
    let reported = false;
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            grn: fieldAgent.agentGrn,
            healthReportedAt: reported ? "2026-01-05T00:00:00Z" : undefined,
          },
        ]),
      ),
    );

    const silent = await renderServerComponent(AgentPage(params));
    expect(screen.getByText("Not recorded")).toBeInTheDocument();
    // updatedAt is the creation time here, and it is not shown as one.
    expect(screen.queryByText("2026-01-03")).not.toBeInTheDocument();
    silent.unmount();

    reported = true;
    await renderServerComponent(AgentPage(params));
    expect(screen.getByText("2026-01-05")).toBeInTheDocument();
    expect(screen.queryByText("Not recorded")).not.toBeInTheDocument();
  });

  it("saves the settings as the next revision and builds the next save on it", async () => {
    const bodies: unknown[] = [];
    server.use(
      http.put(`${ONE}/values`, async ({ request }) => {
        const body = (await request.json()) as { expectedRevision: number };
        bodies.push(body);
        return HttpResponse.json({
          ...fieldAgent,
          revision: body.expectedRevision + 1,
          revisedBy: alice.grn,
        });
      }),
    );
    const user = userEvent.setup();
    await renderServerComponent(AgentPage(params));

    await user.clear(screen.getByLabelText("Value 1"));
    await user.type(screen.getByLabelText("Value 1"), "sonnet");
    await user.click(screen.getByRole("button", { name: "Save settings" }));
    expect(await screen.findByText("Saved as revision 2.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save settings" }));
    expect(await screen.findByText("Saved as revision 3.")).toBeInTheDocument();
    expect(bodies).toEqual([
      { expectedRevision: 1, values: { model: "sonnet" } },
      { expectedRevision: 2, values: { model: "sonnet" } },
    ]);
  });

  it("keeps what was typed when another save landed first, and offers both ways on", async () => {
    let saved = { ...fieldAgent };
    const bodies: { expectedRevision: number }[] = [];
    server.use(
      http.get(ONE, () => HttpResponse.json(saved)),
      http.put(`${ONE}/values`, async ({ request }) => {
        const body = (await request.json()) as {
          expectedRevision: number;
          values: Record<string, string>;
        };
        bodies.push(body);
        if (body.expectedRevision !== saved.revision) {
          return HttpResponse.json(
            {
              kind: "aborted",
              message: "agent definition revision has changed",
            },
            { status: 409 },
          );
        }
        saved = { ...saved, revision: saved.revision + 1, values: body.values };
        return HttpResponse.json(saved);
      }),
    );
    const user = userEvent.setup();
    await renderServerComponent(AgentPage(params));
    // Another editor saves revision 2 after this page was read.
    saved = { ...fieldAgent, revision: 2, values: { model: "opus" } };

    await user.clear(screen.getByLabelText("Value 1"));
    await user.type(screen.getByLabelText("Value 1"), "sonnet");
    await user.click(screen.getByRole("button", { name: "Save settings" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Someone saved revision 2 first. Your edits are still here.",
    );
    expect(screen.getByLabelText("Value 1")).toHaveValue("sonnet");
    expect(
      screen.getByRole("button", { name: "Save settings" }),
    ).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Load revision 2" }));
    expect(screen.getByLabelText("Value 1")).toHaveValue("opus");

    // Loading revision 2 builds the next save on it.
    await user.clear(screen.getByLabelText("Value 1"));
    await user.type(screen.getByLabelText("Value 1"), "sonnet");
    await user.click(screen.getByRole("button", { name: "Save settings" }));
    expect(await screen.findByText("Saved as revision 3.")).toBeInTheDocument();
    expect(bodies.map((body) => body.expectedRevision)).toEqual([1, 2]);
  });
  it("saves over another save only once the person chooses to keep their edits", async () => {
    let saved = { ...fieldAgent };
    const bodies: unknown[] = [];
    server.use(
      http.get(ONE, () => HttpResponse.json(saved)),
      http.put(`${ONE}/values`, async ({ request }) => {
        const body = (await request.json()) as {
          expectedRevision: number;
          values: Record<string, string>;
        };
        bodies.push(body);
        if (body.expectedRevision !== saved.revision) {
          return HttpResponse.json(
            {
              kind: "aborted",
              message: "agent definition revision has changed",
            },
            { status: 409 },
          );
        }
        saved = { ...saved, revision: saved.revision + 1, values: body.values };
        return HttpResponse.json(saved);
      }),
    );
    const user = userEvent.setup();
    await renderServerComponent(AgentPage(params));
    saved = { ...fieldAgent, revision: 2, values: { model: "opus" } };

    await user.clear(screen.getByLabelText("Value 1"));
    await user.type(screen.getByLabelText("Value 1"), "sonnet");
    await user.click(screen.getByRole("button", { name: "Save settings" }));
    await user.click(
      await screen.findByRole("button", { name: "Keep my edits" }),
    );
    expect(screen.getByLabelText("Value 1")).toHaveValue("sonnet");
    await user.click(screen.getByRole("button", { name: "Save settings" }));

    expect(await screen.findByText("Saved as revision 3.")).toBeInTheDocument();
    expect(bodies).toEqual([
      { expectedRevision: 1, values: { model: "sonnet" } },
      { expectedRevision: 2, values: { model: "sonnet" } },
    ]);
  });
});
