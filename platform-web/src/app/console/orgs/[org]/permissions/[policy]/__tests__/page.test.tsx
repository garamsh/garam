import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PolicyPage from "@/app/console/orgs/[org]/permissions/[policy]/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  DENY_DELETES_ID,
  ENGINEERING_ADMINS_ID,
  answerPermissionChecks,
  denyDeletes,
  engineeringAdmins,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import type { IamPolicyDetail } from "@/features/orgs";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => `/console/orgs/acme/permissions/${DENY_DELETES_ID}`,
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const POLICIES = `${API_URL}/orgs/acme/iam-policies`;
const DENY = `${POLICIES}/${DENY_DELETES_ID}`;

/** The guardrail as a draft, which is where Activate is offered. */
const draftGuardrail: IamPolicyDetail = {
  ...denyDeletes,
  policy: { ...denyDeletes.policy, status: "draft" },
};

/** The same guardrail written as a permit, with the permit's actions. */
const permit: IamPolicyDetail = {
  policy: { ...denyDeletes.policy, kind: "permit" },
  versions: denyDeletes.versions.map((version) => ({
    ...version,
    body: { ...version.body, kind: "permit" },
  })),
};

// The console layout mounts the provider a `<Time>` needs.
function renderPolicy(policy: string = DENY_DELETES_ID) {
  return renderServerComponent(
    PolicyPage({ params: Promise.resolve({ org: "acme", policy }) }),
    { wrapper: TooltipProvider },
  );
}

/**
 * The panel of a tab, once that tab is the one selected. Every panel
 * is mounted, and jsdom loads no stylesheet to hide the inactive ones,
 * so a read scoped to this panel is what says which tab it is in.
 */
function panelOf(name: RegExp) {
  expect(screen.getByRole("tab", { name })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  return within(screen.getByRole("tabpanel", { name }));
}

async function openTab(name: RegExp) {
  await userEvent.setup().click(screen.getByRole("tab", { name }));
  return panelOf(name);
}

/** Answers `detail` for the policy at `id`, and the list with it. */
function serve(detail: IamPolicyDetail) {
  server.use(
    http.get(`${POLICIES}/${detail.policy.id}`, () =>
      HttpResponse.json(detail),
    ),
  );
}

describe("policy page", () => {
  it("heads the page with the name, status, kind and identifier, and opens on the Cedar text", async () => {
    await renderPolicy();

    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Guardrail")).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      DENY_DELETES_ID,
    );
    // The default version is v1, whose text names iam:project.delete.
    expect(
      panelOf(/^Definition/).getByLabelText("Cedar text"),
    ).toHaveTextContent(
      'forbid (principal, action == Action::"iam:project.delete", resource);',
    );
    // Cedar text is read, not edited.
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("shows a role link's role, scope, principal and resource", async () => {
    await renderPolicy(ENGINEERING_ADMINS_ID);

    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(screen.getByText("Role link")).toBeInTheDocument();
    const definition = panelOf(/^Definition/);
    expect(definition.getByText("admin")).toBeInTheDocument();
    expect(definition.getByText("Team or project")).toBeInTheDocument();
    expect(definition.getByText("grn:acme/iam/group/eng")).toBeInTheDocument();
    expect(
      definition.getByText("grn:acme/iam/project/frontend"),
    ).toBeInTheDocument();
  });

  it("lists the versions newest first with the default marked", async () => {
    await renderPolicy();

    // Presence: a tab's name carries its count.
    const versions = await openTab(/^Versions/);
    const rows = versions.getAllByRole("row").slice(1);
    // Presence: a row's accessible name is all of its cells.
    expect(rows[0]).toHaveAccessibleName(/^v2/);
    expect(rows[1]).toHaveAccessibleName(/^v1/);
    expect(within(rows[1]!).getByText("In use")).toBeInTheDocument();
    expect(
      within(rows[0]!).getByRole("button", { name: "Make v2 default" }),
    ).toBeInTheDocument();
    expect(
      within(rows[1]!).queryByRole("button", { name: /default$/ }),
    ).not.toBeInTheDocument();
  });

  it("offers deletion in the danger zone, and no attach or edit control", async () => {
    await renderPolicy();

    expect(
      screen.getByRole("button", { name: "Delete policy" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /Attachments/ })).toBeNull();
    expect(
      screen.queryByRole("button", { name: /^Publish/ }),
    ).not.toBeInTheDocument();
  });

  it("offers Activate on a draft and not on an active policy", async () => {
    serve(draftGuardrail);
    const draft = await renderPolicy();
    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Activate" }),
    ).toBeInTheDocument();
    draft.unmount();

    // The control: the same policy, active, offers none.
    serve(denyDeletes);
    await renderPolicy();
    expect(
      screen.queryByRole("button", { name: "Activate" }),
    ).not.toBeInTheDocument();
  });

  it("asks each kind's own write and remove actions, and withdraws only what is denied", async () => {
    const asked: string[] = [];
    server.use(
      answerPermissionChecks((action) => {
        asked.push(action);
        return "deny";
      }),
    );
    serve(draftGuardrail);

    await renderPolicy();

    // The page reads, with every write withdrawn.
    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Activate" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Delete policy" }),
    ).not.toBeInTheDocument();
    const versions = await openTab(/^Versions/);
    expect(
      versions.queryByRole("button", { name: "Make v2 default" }),
    ).not.toBeInTheDocument();
    expect(asked).toEqual(
      expect.arrayContaining([
        "iam:policy.attach",
        "iam:policy.detach",
        "iam:policy.update",
        "iam:policy.delete",
        "iam:permit.write",
      ]),
    );
  });

  it.each([
    {
      detail: draftGuardrail,
      write: "iam:policy.update",
      remove: "iam:policy.delete",
    },
    {
      detail: { ...engineeringAdmins },
      write: "iam:policy.attach",
      remove: "iam:policy.detach",
    },
    {
      detail: { ...permit, policy: { ...permit.policy, status: "draft" } },
      write: "iam:permit.write",
      remove: "iam:policy.delete",
    },
  ] as const)(
    "decides a $detail.policy.kind by $write to write and $remove to remove",
    async ({ detail, write, remove }) => {
      serve(detail as IamPolicyDetail);
      // Every other action is allowed, so only the named one can
      // withdraw a control.
      server.use(
        answerPermissionChecks((action) =>
          action === write ? "deny" : "allow",
        ),
      );
      const writeDenied = await renderPolicy(detail.policy.id);
      expect(
        screen.queryByRole("button", { name: "Activate" }),
      ).not.toBeInTheDocument();
      // The control: the removal is still offered.
      expect(
        screen.getByRole("button", { name: "Delete policy" }),
      ).toBeInTheDocument();
      writeDenied.unmount();

      server.use(
        answerPermissionChecks((action) =>
          action === remove ? "deny" : "allow",
        ),
      );
      await renderPolicy(detail.policy.id);
      expect(
        screen.queryByRole("button", { name: "Delete policy" }),
      ).not.toBeInTheDocument();
      // The control: the write is still offered.
      expect(
        screen.getByRole("button", { name: "Activate" }),
      ).toBeInTheDocument();
    },
  );

  it("offers every write when the check could not be asked", async () => {
    serve(draftGuardrail);
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.error(),
      ),
    );

    await renderPolicy();

    expect(
      screen.getByRole("button", { name: "Activate" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Delete policy" }),
    ).toBeInTheDocument();
  });

  it("renders not-found for a policy the service does not hold", async () => {
    // The control: a policy it holds renders under its name.
    const listed = await renderPolicy();
    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    listed.unmount();

    server.use(
      http.get(`${POLICIES}/p-missing`, () =>
        HttpResponse.json(
          { kind: "not_found", message: "no such policy" },
          { status: 404 },
        ),
      ),
    );

    await expect(renderPolicy("p-missing")).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the policy is refused rather than empty when garam answers 403", async () => {
    server.use(
      http.get(DENY, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderPolicy();

    expect(screen.getByText("You can't view this policy")).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", {
        level: 1,
        name: "Deny project deletion",
      }),
    ).not.toBeInTheDocument();
  });

  it("says the API is unreachable rather than showing a policy", async () => {
    server.use(http.get(DENY, () => HttpResponse.error()));

    await renderPolicy();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
  });

  it("says an activation landed beside the header once its button is gone", async () => {
    serve(draftGuardrail);
    server.use(
      http.post(
        `${DENY}/activate`,
        () => new HttpResponse(null, { status: 204 }),
      ),
    );
    const user = userEvent.setup();
    await renderPolicy();
    const activate = screen.getByRole("button", { name: "Activate" });
    // The header: the smallest element holding the policy's name and
    // the control that activates it. The wait below reads only this, so
    // each of its checks stays cheap on a page this large.
    let header = screen.getByRole("heading", { level: 1 }).parentElement;
    while (header !== null && !header.contains(activate)) {
      header = header.parentElement;
    }
    if (header === null) throw new Error("no header holds Activate");

    await user.click(activate);
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Activate policy" }),
    );

    expect(await within(header).findByText("Activated v1")).toBeInTheDocument();
  });
});
