import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AttachmentsPage from "@/app/console/orgs/[org]/permissions/attachments/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  alice,
  answerPermissionChecks,
  denyDeletes,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/policies`;

function renderAttachments(subject?: string) {
  return renderServerComponent(
    AttachmentsPage({
      params: Promise.resolve({ org: "acme" }),
      searchParams: Promise.resolve(subject === undefined ? {} : { subject }),
    }),
    { wrapper: TooltipProvider },
  );
}

function refuse() {
  return HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 });
}

/** The section of the inspection under the heading `name`. */
function section(name: string): HTMLElement {
  return screen.getByRole("region", { name });
}

describe("attachments page", () => {
  it("shows whom each policy is attached to, and everyone for none", async () => {
    await renderAttachments();

    // Presence: a row's accessible name is all of its cells.
    const attached = screen.getByRole("row", { name: /Deny project deletion/ });
    expect(
      within(attached).getByRole("link", { name: "Engineering (Team)" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=team%3Aeng",
    );
    // Presence, for the same reason.
    const everyone = screen.getByRole("row", { name: /Baseline read/ });
    expect(
      within(everyone).getByText("Everyone in this organization"),
    ).toBeInTheDocument();
  });

  it("keeps a person's direct, team and organization-wide policies apart", async () => {
    await renderAttachments("person:alice");

    expect(
      screen.getByRole("heading", { level: 2, name: "Alice · Person" }),
    ).toBeInTheDocument();
    expect(
      within(section("Attached directly")).getByText(
        "No policy is attached to Alice.",
      ),
    ).toBeInTheDocument();
    const through = section("Through teams");
    expect(
      within(through).getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(
      within(through).getByRole("link", { name: "Engineering" }),
    ).toBeInTheDocument();
    expect(
      within(section("Organization-wide")).getByRole("link", {
        name: "Baseline read",
      }),
    ).toBeInTheDocument();
    // Presence: the caveat is one sentence and this is a clause of it.
    expect(
      screen.getByText(/this is not a final access decision/),
    ).toBeInTheDocument();
  });

  it("says a team's members were not checked rather than that no team applies", async () => {
    // The control: with the team's members read, its policy reaches
    // Alice through it.
    const allowed = await renderAttachments("person:alice");
    expect(
      within(section("Through teams")).getByRole("link", {
        name: "Deny project deletion",
      }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(http.get(`${API_URL}/orgs/acme/groups/eng/members`, refuse));

    await renderAttachments("person:alice");

    const through = section("Through teams");
    expect(
      within(through).queryByText("No policy reaches Alice through a team."),
    ).not.toBeInTheDocument();
    // Presence: the sentence goes on to name the team and why.
    expect(
      within(through).getByText(/^Not checked: the members of/),
    ).toHaveTextContent("Engineering");
  });

  it("claims no absence for a subject while a policy's attachments are unread", async () => {
    const BASELINE = `${POLICIES}/0a1b2c3d4e5f6071/attachments`;
    // The control: with every policy's attachments read and none
    // reaching everyone, the empty sections say so without qualifying.
    server.use(
      http.get(BASELINE, () =>
        HttpResponse.json([
          {
            policyGrn: "grn:acme:default:policy:0a1b2c3d4e5f6071",
            subjectGrn: reportedAgent.grn,
            createdAt: "2026-01-02T03:04:05Z",
          },
        ]),
      ),
    );
    const read = await renderAttachments("person:alice");
    expect(
      within(section("Organization-wide")).getByText(
        "No active policy applies to everyone in this organization.",
      ),
    ).toBeInTheDocument();
    expect(
      within(section("Attached directly")).getByText(
        "No policy is attached to Alice.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Some policies were not checked"),
    ).not.toBeInTheDocument();
    read.unmount();

    server.use(http.get(BASELINE, refuse));

    await renderAttachments("person:alice");

    const orgWide = section("Organization-wide");
    expect(
      within(orgWide).getByText(
        "No active policy you can read applies to everyone in this organization.",
      ),
    ).toBeInTheDocument();
    expect(
      within(orgWide).queryByRole("link", { name: "Baseline read" }),
    ).not.toBeInTheDocument();
    expect(
      within(section("Attached directly")).getByText(
        "No policy you can read is attached to Alice.",
      ),
    ).toBeInTheDocument();
    // Presence: the notice names the policy inside a sentence.
    expect(
      screen.getByText("Some policies were not checked").closest("[role]"),
    ).toHaveTextContent(
      "The attachments of Baseline read are not listed to you",
    );
  });

  it("says a subject whose list was refused cannot be shown, not that it is missing", async () => {
    // The control: with the people listed, the same address shows her.
    const listed = await renderAttachments("person:alice");
    expect(
      screen.getByRole("heading", { level: 2, name: "Alice · Person" }),
    ).toBeInTheDocument();
    listed.unmount();

    server.use(http.get(`${API_URL}/orgs/acme/members`, refuse));

    await renderAttachments("person:alice");

    expect(
      screen.getByText("You can't view this organization's people"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        "No team, person or agent of this organization matches this address.",
      ),
    ).not.toBeInTheDocument();
  });

  it("addresses subjects by kind and identifier, never by GRN", async () => {
    const { container } = await renderAttachments(alice.grn);

    expect(
      screen.getByText(
        "No team, person or agent of this organization matches this address.",
      ),
    ).toBeInTheDocument();
    // The address the picker sends: the subject's kind and identifier.
    const user = userEvent.setup();
    await user.click(
      screen.getByRole("combobox", { name: "Team, person or agent" }),
    );
    await user.click(
      await screen.findByRole("option", { name: /^Alice · Person/ }),
    );
    const picker = screen
      .getByRole("button", { name: "Show policies" })
      .closest("form");
    expect(picker).not.toBeNull();
    expect(new FormData(picker ?? undefined).get("subject")).toBe(
      "person:alice",
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("withdraws a subject's attach and detach only where the server denies them", async () => {
    // The control: allowed, Engineering's direct policy is detachable
    // and another policy is offered to attach.
    const allowed = await renderAttachments("team:eng");
    const direct = () => section("Attached directly");
    expect(
      within(direct()).getByRole("button", {
        name: "Detach Deny project deletion from Engineering",
      }),
    ).toBeInTheDocument();
    expect(
      within(direct()).getByRole("combobox", { name: "Attach a policy" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(answerPermissionChecks(() => "deny"));
    await renderAttachments("team:eng");

    expect(
      within(direct()).queryByRole("button", { name: /^Detach/ }),
    ).not.toBeInTheDocument();
    expect(
      within(direct()).queryByRole("combobox", { name: "Attach a policy" }),
    ).not.toBeInTheDocument();
    // The attachment itself is still listed.
    expect(
      within(direct()).getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
  });

  it("asks at most the API's limit and leaves the policies past it to the server", async () => {
    const policies = Array.from({ length: 30 }, (_, index) => ({
      ...denyDeletes,
      grn: `grn:acme:default:policy:p${index + 1}`,
      name: `Policy ${index + 1}`,
      status: "draft",
    }));
    const asked: number[] = [];
    server.use(
      http.get(POLICIES, () => HttpResponse.json(policies)),
      http.get(`${POLICIES}/:policy/attachments`, () => HttpResponse.json([])),
      http.post(
        `${API_URL}/orgs/acme/permission-checks`,
        async ({ request }) => {
          const body = await request.json();
          const checks =
            typeof body === "object" &&
            body !== null &&
            "checks" in body &&
            Array.isArray(body.checks)
              ? body.checks
              : [];
          asked.push(checks.length);
          return HttpResponse.json({
            results: checks.map(() => ({ action: "x", decision: "deny" })),
          });
        },
      ),
    );
    const user = userEvent.setup();

    await renderAttachments("team:eng");

    // Two checks for each of the first 25 policies, the API's 50.
    expect(asked).toEqual([50]);
    await user.click(
      within(section("Attached directly")).getByRole("combobox", {
        name: "Attach a policy",
      }),
    );
    // Asked and denied, the first is withdrawn; unasked, the last is
    // offered and the server decides.
    expect(
      screen.queryByRole("option", { name: "Policy 1 · Draft" }),
    ).not.toBeInTheDocument();
    expect(
      await screen.findByRole("option", { name: "Policy 30 · Draft" }),
    ).toBeInTheDocument();
  });
});
