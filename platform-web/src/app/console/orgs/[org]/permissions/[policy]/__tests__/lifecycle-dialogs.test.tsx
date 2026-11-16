import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { ActivatePolicyDialog } from "@/app/console/orgs/[org]/permissions/[policy]/_components/activate-policy-dialog";
import { DeletePolicyDialog } from "@/app/console/orgs/[org]/permissions/[policy]/_components/delete-policy-dialog";
import { MakeDefaultDialog } from "@/app/console/orgs/[org]/permissions/[policy]/_components/make-default-dialog";
import { Outcome, OutcomeProvider } from "@/app/console/_components/outcome";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";
import {
  API_URL,
  DENY_DELETES_ID,
  ENGINEERING_ADMINS_ID,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const DENY = `${API_URL}/orgs/acme/iam-policies/${DENY_DELETES_ID}`;
/** The policy the same write is accepted for: the control. */
const ACCEPTED = `${API_URL}/orgs/acme/iam-policies/${ENGINEERING_ADMINS_ID}`;
const V1 =
  'forbid (principal, action == Action::"iam:project.delete", resource);';
const V2 =
  'forbid (principal, action == Action::"iam:project.update", resource);';

describe("activate policy dialog", () => {
  it("states the version it decides by and posts the activation", async () => {
    const activated: string[] = [];
    server.use(
      http.post(`${DENY}/activate`, () => {
        activated.push("deny");
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(
      <ActivatePolicyDialog
        org="acme"
        policy={DENY_DELETES_ID}
        policyName="Deny project deletion"
        version={2}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Activate" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Activate Deny project deletion?",
    });

    expect(dialog).toHaveAccessibleDescription(
      "v2 decides for this organization from now on.",
    );
    expect(activated).toEqual([]);
    await user.click(
      within(dialog).getByRole("button", { name: "Activate policy" }),
    );
    await waitFor(() => expect(activated).toEqual(["deny"]));
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("keeps a refusal in the dialog and does not say it activated", async () => {
    server.use(
      http.post(`${DENY}/activate`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "denied: iam:policy.update" },
          { status: 403 },
        ),
      ),
      http.post(
        `${ACCEPTED}/activate`,
        () => new HttpResponse(null, { status: 204 }),
      ),
    );
    const user = userEvent.setup();
    const activate = (policy: string) => (
      <OutcomeProvider>
        <Outcome area="lifecycle" />
        <ActivatePolicyDialog
          org="acme"
          policy={policy}
          policyName="Deny project deletion"
          version={1}
        />
      </OutcomeProvider>
    );
    const refused = render(activate(DENY_DELETES_ID));

    await user.click(screen.getByRole("button", { name: "Activate" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Activate policy" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(screen.queryByText("Activated v1")).not.toBeInTheDocument();
    refused.unmount();

    // The control: the same control for a policy the service accepts
    // does say it activated, so the absence above is the refusal's.
    render(activate(ENGINEERING_ADMINS_ID));
    await user.click(screen.getByRole("button", { name: "Activate" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Activate policy",
      }),
    );
    expect(await screen.findByText("Activated v1")).toBeInTheDocument();
  });
});

describe("make default dialog", () => {
  const props = {
    org: "acme",
    policy: DENY_DELETES_ID,
    currentText: V2,
    targetText: V1,
  };

  it("names both versions, shows the change and posts the chosen one", async () => {
    const Rollback = z.strictObject({ version: z.number() });
    const posted: z.infer<typeof Rollback>[] = [];
    server.use(
      http.post(`${DENY}/rollback`, async ({ request }) => {
        posted.push(Rollback.parse(await request.json()));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(<MakeDefaultDialog {...props} current={2} target={1} active />);

    await user.click(screen.getByRole("button", { name: "Roll back to v1" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Roll back to v1?",
    });
    expect(dialog).toHaveAccessibleDescription(
      "v1 decides for this organization from now on.",
    );
    // The diff says what the rollback removes and adds.
    const diff = within(dialog).getByRole("figure");
    expect(diff).toHaveTextContent("iam:project.update");
    expect(diff).toHaveTextContent("iam:project.delete");

    await user.click(
      within(dialog).getByRole("button", { name: "Roll back to v1" }),
    );
    await waitFor(() => expect(posted).toEqual([{ version: 1 }]));
    // Without a page to say it elsewhere, it is said beside the
    // control.
    expect(await screen.findByText("Rolled back to v1.")).toBeInTheDocument();
  });

  it("says a new default in the versions area, which outlives its row", async () => {
    server.use(
      http.post(
        `${DENY}/rollback`,
        () => new HttpResponse(null, { status: 204 }),
      ),
    );
    const user = userEvent.setup();
    render(
      <OutcomeProvider>
        <Outcome area="versions" />
        <MakeDefaultDialog {...props} current={1} target={2} active />
      </OutcomeProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Make v2 default" }),
    );

    const said = await screen.findByText("v2 is now the default.");
    expect(said.closest("[role=status]")).toBeInTheDocument();
  });

  it("keeps a refusal in the dialog and does not say the default moved", async () => {
    server.use(
      http.post(`${DENY}/rollback`, () =>
        HttpResponse.json(
          { kind: "unavailable", message: "policy service down" },
          { status: 503 },
        ),
      ),
      http.post(
        `${ACCEPTED}/rollback`,
        () => new HttpResponse(null, { status: 204 }),
      ),
    );
    const user = userEvent.setup();
    const makeDefault = (policy: string) => (
      <OutcomeProvider>
        <Outcome area="versions" />
        <MakeDefaultDialog
          {...props}
          policy={policy}
          current={1}
          target={2}
          active
        />
      </OutcomeProvider>
    );
    const refused = render(makeDefault(DENY_DELETES_ID));

    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Make v2 default" }),
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.unreachable,
    );
    expect(
      screen.queryByText("v2 is now the default."),
    ).not.toBeInTheDocument();
    refused.unmount();

    // The control: the same control for a policy the service accepts
    // does say the default moved, so the absence above is the
    // refusal's.
    render(makeDefault(ENGINEERING_ADMINS_ID));
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Make v2 default",
      }),
    );
    expect(
      await screen.findByText("v2 is now the default."),
    ).toBeInTheDocument();
  });

  it("says nothing applies yet for a draft, and decides at once for an active policy", async () => {
    const user = userEvent.setup();
    const active = render(
      <MakeDefaultDialog {...props} current={1} target={2} active />,
    );
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    expect(await screen.findByRole("alertdialog")).toHaveAccessibleDescription(
      "v2 decides for this organization from now on.",
    );
    active.unmount();

    // The control: the same change on a draft evaluates nothing.
    render(
      <MakeDefaultDialog {...props} current={1} target={2} active={false} />,
    );
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    expect(await screen.findByRole("alertdialog")).toHaveAccessibleDescription(
      "Nothing applies until activated.",
    );
  });
});

describe("delete policy dialog", () => {
  it("offers the delete only once the policy's whole name is typed", async () => {
    let deleted = 0;
    server.use(
      http.delete(DENY, () => {
        deleted += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(
      <DeletePolicyDialog
        org="acme"
        policy={DENY_DELETES_ID}
        policyName="Deny project deletion"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete policy" }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Delete Deny project deletion?",
    });
    const confirm = within(dialog).getByRole("button", {
      name: "Delete policy",
    });
    expect(confirm).toBeDisabled();

    await user.type(within(dialog).getByRole("textbox"), "Deny project");
    expect(confirm).toBeDisabled();
    expect(deleted).toBe(0);
    // The control: the whole name enables it. The delete it sends, and
    // the list it opens, are the server action's (policy-lifecycle).
    await user.type(within(dialog).getByRole("textbox"), " deletion");
    expect(confirm).toBeEnabled();
  });

  it("keeps a refusal in the dialog until it closes, and opens again without it", async () => {
    server.use(
      http.delete(DENY, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "denied: iam:policy.delete" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(
      <DeletePolicyDialog
        org="acme"
        policy={DENY_DELETES_ID}
        policyName="Deny project deletion"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete policy" }));
    const dialog = screen.getByRole("alertdialog");
    await user.type(
      within(dialog).getByRole("textbox"),
      "Deny project deletion",
    );
    await user.click(
      within(dialog).getByRole("button", { name: "Delete policy" }),
    );
    // The control: the refusal is said, in the dialog that asked.
    expect(
      await within(screen.getByRole("alertdialog")).findByRole("alert"),
    ).toHaveTextContent(REFUSAL_COPY.permissionDenied);

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Delete policy" }));

    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
