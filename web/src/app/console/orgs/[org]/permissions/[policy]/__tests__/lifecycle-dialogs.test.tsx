import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { ActivatePolicyDialog } from "@/app/console/orgs/[org]/permissions/[policy]/_components/activate-policy-dialog";
import { ArchivePolicyDialog } from "@/app/console/orgs/[org]/permissions/[policy]/_components/archive-policy-dialog";
import { MakeDefaultDialog } from "@/app/console/orgs/[org]/permissions/[policy]/_components/make-default-dialog";
import { Outcome, OutcomeProvider } from "@/app/console/_components/outcome";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { API_URL, baselineRead, denyDeletes } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const BASELINE = `${API_URL}/orgs/acme/policies/0a1b2c3d4e5f6071`;
const DENY = `${API_URL}/orgs/acme/policies/d1e2f3a4b5c6d7e8`;
const EVERYONE = "Applies to everyone: it has no attachments.";
const V1 = '{\n  "Effect": "Deny"\n}';
const V2 = '{\n  "Effect": "Allow"\n}';

describe("activate policy dialog", () => {
  it("states the version and the scope, and asks to confirm everyone", async () => {
    const activated: string[] = [];
    server.use(
      http.post(`${BASELINE}/activate`, () => {
        activated.push("baseline");
        return HttpResponse.json(baselineRead);
      }),
    );
    const user = userEvent.setup();
    render(
      <ActivatePolicyDialog
        org="acme"
        policy="0a1b2c3d4e5f6071"
        policyName="Baseline read"
        version={1}
        reach={EVERYONE}
        orgWide
        conditioned={false}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Activate" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Activate Baseline read?",
    });

    expect(dialog).toHaveAccessibleDescription(EVERYONE);
    const confirm = within(dialog).getByRole("checkbox", {
      name: "Apply it to everyone",
    });
    expect(confirm).toBeRequired();
    await user.click(confirm);
    await user.click(
      within(dialog).getByRole("button", { name: "Activate for everyone" }),
    );
    await waitFor(() => expect(activated).toEqual(["baseline"]));
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });

  it("asks nothing more of an attached policy", async () => {
    const user = userEvent.setup();
    render(
      <ActivatePolicyDialog
        org="acme"
        policy="d1e2f3a4b5c6d7e8"
        policyName="Deny project deletion"
        version={1}
        reach="Applies to Engineering (Team)."
        orgWide={false}
        conditioned={false}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Activate" }));
    const dialog = await screen.findByRole("alertdialog");

    expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Activate policy" }),
    ).toBeInTheDocument();
  });
});

describe("make default dialog", () => {
  const props = {
    org: "acme",
    policy: "d1e2f3a4b5c6d7e8",
    currentText: V2,
    targetText: V1,
    reach: "Applies to Engineering (Team).",
    orgWide: false,
  };

  it("names both versions, shows the change and posts the chosen one", async () => {
    const Rollback = z.strictObject({ version: z.number() });
    const posted: z.infer<typeof Rollback>[] = [];
    server.use(
      http.post(`${DENY}/rollback`, async ({ request }) => {
        posted.push(Rollback.parse(await request.json()));
        return HttpResponse.json(denyDeletes);
      }),
    );
    const user = userEvent.setup();
    render(
      <MakeDefaultDialog
        {...props}
        current={2}
        target={1}
        active
        conditioned={false}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Roll back to v1" }));
    const dialog = await screen.findByRole("alertdialog", {
      name: "Roll back to v1?",
    });
    expect(dialog).toHaveAccessibleDescription(
      "Applies to Engineering (Team).",
    );
    // The diff says what the rollback removes and adds, in words.
    const diff = within(dialog).getByRole("figure");
    expect(diff).toHaveTextContent('Removed: "Effect": "Allow"');
    expect(diff).toHaveTextContent('Added: "Effect": "Deny"');

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
      http.post(`${DENY}/rollback`, () => HttpResponse.json(denyDeletes)),
    );
    const user = userEvent.setup();
    render(
      <OutcomeProvider>
        <Outcome area="versions" />
        <MakeDefaultDialog
          {...props}
          current={1}
          target={2}
          active
          conditioned={false}
        />
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

  it("says the version to evaluate carries a Condition that restricts nothing", async () => {
    const user = userEvent.setup();
    // The control: without a Condition, the dialog carries no notice.
    const plain = render(
      <MakeDefaultDialog
        {...props}
        current={1}
        target={2}
        active
        conditioned={false}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    expect(
      screen.queryByText(
        "Conditions are ignored: the statement applies as if it had none",
      ),
    ).not.toBeInTheDocument();
    plain.unmount();

    render(
      <MakeDefaultDialog
        {...props}
        current={1}
        target={2}
        active
        conditioned
      />,
    );
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));

    expect(
      await screen.findByText(
        "Conditions are ignored: the statement applies as if it had none",
      ),
    ).toBeInTheDocument();
  });

  it("carries the everyone confirmation for an active policy with no attachment", async () => {
    const user = userEvent.setup();
    const active = render(
      <MakeDefaultDialog
        {...props}
        reach={EVERYONE}
        orgWide
        current={1}
        target={2}
        active
        conditioned={false}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    expect(
      await screen.findByRole("checkbox", {
        name: "Apply it to everyone",
      }),
    ).toBeRequired();
    active.unmount();

    // The control: the same change on a draft evaluates nothing, so it
    // carries no such confirmation.
    render(
      <MakeDefaultDialog
        {...props}
        reach={EVERYONE}
        orgWide
        current={1}
        target={2}
        active={false}
        conditioned={false}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Make v2 default" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
    expect(dialog).toHaveAccessibleDescription(
      "Nothing applies until activated.",
    );
  });
});

describe("archive policy dialog", () => {
  it("says the archive landed in the lifecycle area", async () => {
    server.use(
      http.post(`${DENY}/archive`, () =>
        HttpResponse.json({ ...denyDeletes, status: "archived" }),
      ),
    );
    const user = userEvent.setup();
    render(
      <OutcomeProvider>
        <Outcome area="lifecycle" />
        <ArchivePolicyDialog
          org="acme"
          policy="d1e2f3a4b5c6d7e8"
          policyName="Deny project deletion"
        />
      </OutcomeProvider>,
    );
    // The control: nothing is said before the write.
    expect(screen.queryByText(/^Archived/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Archive policy" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Archive policy" }),
    );

    expect(
      await screen.findByText("Archived. It can be activated again."),
    ).toBeInTheDocument();
  });

  it("keeps a refusal in the dialog until it closes, and opens again without it", async () => {
    server.use(
      http.post(`${DENY}/archive`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "denied: policy:archive" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(
      <ArchivePolicyDialog
        org="acme"
        policy="d1e2f3a4b5c6d7e8"
        policyName="Deny project deletion"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Archive policy" }));
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "Archive policy",
      }),
    );
    // The control: the refusal is said, in the dialog that asked.
    expect(
      await within(screen.getByRole("alertdialog")).findByRole("alert"),
    ).toHaveTextContent(REFUSAL_COPY.permissionDenied);

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Archive policy" }));

    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
