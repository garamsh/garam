import { render, screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import {
  YourAccess,
  readAccess,
  type AccessAction,
} from "@/app/console/_components/your-access";
import {
  API_URL,
  answerExplanations,
  answerPermissionChecks,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const AGENT = { type: "agent", id: "9f2ac1b40d8e7a35" } as const;

const ACTIONS: AccessAction[] = [
  {
    key: "rename",
    label: "Rename it",
    check: { action: "agent:rename", resource: AGENT },
  },
  {
    key: "replace",
    label: "Replace its host",
    check: { action: "agent:replace", resource: AGENT },
  },
];

async function renderPanel(actions: AccessAction[] = ACTIONS) {
  render(<YourAccess readings={await readAccess("acme", actions)} />);
  return screen.getByRole("region", { name: "Your access" });
}

/** The panel's row for one action, by its label. */
function row(panel: HTMLElement, label: string): HTMLElement {
  const item = within(panel).getByText(label).closest("li");
  if (!item) throw new Error(`no row for ${label}`);
  return item;
}

describe("Your access", () => {
  it("names the grant behind an allowed action", async () => {
    server.use(
      answerExplanations(() => ({
        decision: "allow",
        basis: "statements",
        statements: [
          {
            effect: "Allow",
            source: "policy",
            policy: "platform-admins",
            attachment: "caller",
          },
        ],
      })),
    );

    const panel = await renderPanel(ACTIONS.slice(0, 1));

    const rename = row(panel, "Rename it");
    expect(rename).toHaveTextContent("Allowed");
    expect(rename).toHaveTextContent("Policy platform-admins, attached to you");
  });

  it("keeps a denied action listed with its reason and who can take it", async () => {
    server.use(
      answerPermissionChecks(() => "deny"),
      answerExplanations(() => ({
        decision: "deny",
        basis: "statements",
        statements: [],
      })),
      http.get(`${API_URL}/orgs/acme/permission-holders`, () =>
        HttpResponse.json({
          holders: [
            {
              type: "user",
              grn: "grn:root:default:user:minji",
              nickname: "Minji",
            },
          ],
          nextOffset: 50,
        }),
      ),
    );

    const panel = await renderPanel(ACTIONS.slice(0, 1));

    const rename = row(panel, "Rename it");
    expect(rename).toHaveTextContent("Not allowed");
    expect(rename).toHaveTextContent("No role or policy grants it to you.");
    expect(rename).toHaveTextContent(/Who can:.*Minji.*person/);
    // The first page of candidates is all that is asked.
    expect(rename).toHaveTextContent("and others past the first fifty asked");
  });

  it("says who holds it is not shown where garam refuses the caller that list", async () => {
    server.use(
      answerPermissionChecks(() => "deny"),
      answerExplanations(() => ({
        decision: "deny",
        basis: "statements",
        statements: [],
      })),
      http.get(`${API_URL}/orgs/acme/permission-holders`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    const panel = await renderPanel(ACTIONS.slice(0, 1));

    expect(row(panel, "Rename it")).toHaveTextContent(
      "Who holds it isn't shown to you.",
    );
  });

  it("says a policy the caller cannot read only as such", async () => {
    server.use(
      answerPermissionChecks(() => "deny"),
      answerExplanations(() => ({
        decision: "deny",
        basis: "statements",
        statements: [{ effect: "Deny", source: "unreadable-policy" }],
      })),
    );

    const panel = await renderPanel(ACTIONS.slice(0, 1));

    expect(row(panel, "Rename it")).toHaveTextContent(
      "Denied by A policy you can't read, by a path garam doesn't show you",
    );
  });

  it("leaves an action the operation decides to the operation, and asks no holders", async () => {
    let holdersAsked = 0;
    server.use(
      answerPermissionChecks((action) =>
        action === "agent:replace" ? "unknown" : "allow",
      ),
      answerExplanations((action) =>
        action === "agent:replace"
          ? { decision: "unknown", basis: "operation", statements: [] }
          : {
              decision: "allow",
              basis: "statements",
              statements: [
                { effect: "Allow", source: "org-role", roles: ["owner"] },
              ],
            },
      ),
      http.get(`${API_URL}/orgs/acme/permission-holders`, () => {
        holdersAsked += 1;
        return HttpResponse.json({ holders: [] });
      }),
    );

    const panel = await renderPanel();

    const replace = row(panel, "Replace its host");
    expect(replace).toHaveTextContent("Decided when you act");
    expect(replace).toHaveTextContent(/The operation decides this/);
    expect(holdersAsked).toBe(0);
  });

  it("keeps the check's answer where garam does not explain it", async () => {
    server.use(
      answerPermissionChecks(() => "allow"),
      http.get(`${API_URL}/orgs/acme/permission-explanation`, () =>
        HttpResponse.error(),
      ),
    );

    const panel = await renderPanel(ACTIONS.slice(0, 1));

    const rename = row(panel, "Rename it");
    expect(rename).toHaveTextContent("Allowed");
    expect(rename).toHaveTextContent(
      "Why was not answered: garam didn't explain it.",
    );
  });

  it("checks every action in one request", async () => {
    let checks = 0;
    server.use(
      // Counts and falls through to the default answer.
      http.post(`${API_URL}/orgs/acme/permission-checks`, () => {
        checks += 1;
      }),
    );

    await renderPanel();

    expect(checks).toBe(1);
  });
});
