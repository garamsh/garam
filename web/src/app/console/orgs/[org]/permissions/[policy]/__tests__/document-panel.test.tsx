import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DocumentPanel } from "@/app/console/orgs/[org]/permissions/[policy]/_components/document-panel";
import { OutcomeProvider } from "@/app/console/orgs/[org]/permissions/_components/outcome";
import { formatPolicyDocument } from "@/lib/policy-document";
import { enter } from "@/testing/enter";
import {
  API_URL,
  denyDeletes,
  denyDeletesVersions,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const DENY = `${API_URL}/orgs/acme/policies/d1e2f3a4b5c6d7e8`;

const DEFAULT_TEXT = formatPolicyDocument(denyDeletesVersions[0].document);

/** The panel as the policy page holds it, under its outcome provider. */
function renderPanel() {
  return render(
    <OutcomeProvider>
      <DocumentPanel
        org="acme"
        policy="d1e2f3a4b5c6d7e8"
        policyName="Deny project deletion"
        active
        defaultVersion={1}
        nextVersion={3}
        defaultText={DEFAULT_TEXT}
        reach="Applies to Engineering (Team)."
        orgWide={false}
        canUpdate
      />
    </OutcomeProvider>,
  );
}

/** Changes the first statement's first Action, as a person would. */
async function editAction(
  user: ReturnType<typeof userEvent.setup>,
  to: string,
) {
  const action = screen.getByRole("textbox", { name: "Statement 1 action 1" });
  await user.clear(action);
  await enter(user, action, to);
}

describe("document panel", () => {
  it("publishes the edit as the next version after showing what it changes", async () => {
    const posted: string[] = [];
    server.use(
      http.post(`${DENY}/versions`, async ({ request }) => {
        posted.push(JSON.stringify(await request.json()));
        return HttpResponse.json(
          { ...denyDeletesVersions[1], version: 3 },
          { status: 201 },
        );
      }),
      http.post(`${DENY}/rollback`, () =>
        HttpResponse.json({ ...denyDeletes, defaultVersion: 3 }),
      ),
    );
    const user = userEvent.setup();
    renderPanel();

    // Nothing to publish until the document differs from the default.
    const publish = screen.getByRole("button", { name: "Publish v3" });
    expect(publish).toBeDisabled();
    await editAction(user, "project:update");
    expect(publish).toBeEnabled();
    expect(screen.getByText("Edited, not published")).toBeInTheDocument();

    await user.click(publish);
    const dialog = await screen.findByRole("alertdialog", {
      name: "Publish v3 of Deny project deletion?",
    });
    expect(dialog).toHaveAccessibleDescription(
      "It becomes the default and applies at once. Applies to Engineering (Team).",
    );
    const diff = within(dialog).getByRole("figure");
    expect(diff).toHaveTextContent('Removed: "project:delete"');
    expect(diff).toHaveTextContent('Added: "project:update"');

    await user.click(
      within(dialog).getByRole("button", { name: "Publish v3" }),
    );

    // Success is said beside the control that opened the dialog.
    expect(await screen.findByText("Published v3.")).toBeInTheDocument();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(posted).toHaveLength(1);
    expect(posted[0]).toContain("project:update");
  });

  it("closes the dialog on a refused document and marks it on the editor", async () => {
    server.use(
      http.post(`${DENY}/versions`, () =>
        HttpResponse.json(
          { kind: "invalid_argument", message: "bad" },
          { status: 400 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderPanel();

    await editAction(user, "nope:nope");
    await user.click(screen.getByRole("button", { name: "Publish v3" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Publish v3" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    const editor = screen.getByRole("group", { name: "Document" });
    expect(within(editor).getByRole("alert")).toHaveTextContent(
      /^This policy document can't be saved\./,
    );
  });

  it("keeps a refusal that is not the document's in the dialog", async () => {
    server.use(
      http.post(`${DENY}/versions`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderPanel();

    await editAction(user, "project:update");
    await user.click(screen.getByRole("button", { name: "Publish v3" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Publish v3" }),
    );

    // Presence: the refusal is the generic permission sentence.
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      /^You don't have permission/,
    );
    // The page behind the open dialog is hidden from the tree.
    const editor = screen.getByRole("group", {
      name: "Document",
      hidden: true,
    });
    expect(
      within(editor).queryByRole("alert", { hidden: true }),
    ).not.toBeInTheDocument();
  });

  it("saves without publishing and says the default did not move", async () => {
    server.use(
      http.post(`${DENY}/versions`, () =>
        HttpResponse.json(
          { ...denyDeletesVersions[1], version: 3 },
          { status: 201 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderPanel();

    await editAction(user, "project:update");
    await user.click(
      screen.getByRole("button", { name: "Save without publishing" }),
    );

    expect(
      await screen.findByText("Saved as v3. v1 is still the default."),
    ).toBeInTheDocument();
  });

  it("offers to finish a half-done publish rather than to append it again", async () => {
    const rollbacks: unknown[] = [];
    let refusing = true;
    server.use(
      http.post(`${DENY}/versions`, () =>
        HttpResponse.json(
          { ...denyDeletesVersions[1], version: 3 },
          { status: 201 },
        ),
      ),
      http.post(`${DENY}/rollback`, async ({ request }) => {
        rollbacks.push(await request.json());
        return refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "no" },
              { status: 403 },
            )
          : HttpResponse.json({ ...denyDeletes, defaultVersion: 3 });
      }),
    );
    const user = userEvent.setup();
    renderPanel();

    await editAction(user, "project:update");
    await user.click(screen.getByRole("button", { name: "Publish v3" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Publish v3" }),
    );

    // Presence: the sentence goes on to quote why the default did not
    // move.
    const halfDone = await screen.findByText(
      /^Saved as v3, but it is not the default:/,
    );
    expect(halfDone).toHaveTextContent(/You don't have permission/);
    // Publishing the same text again would append a duplicate v4.
    expect(screen.getByRole("button", { name: "Publish v3" })).toBeDisabled();

    // The control: the offered step makes v3 the default.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Make v3 default" }));
    const finish = await screen.findByRole("alertdialog", {
      name: "Make v3 the default?",
    });
    await user.click(
      within(finish).getByRole("button", { name: "Make v3 default" }),
    );
    expect(
      await screen.findByText("v3 is now the default."),
    ).toBeInTheDocument();
    expect(rollbacks).toEqual([{ version: 3 }, { version: 3 }]);
  });

  it("compares the document as stored, so a reformatted paste is no change", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.click(screen.getByRole("tab", { name: "JSON" }));
    const json = await screen.findByRole("textbox", { name: "Document JSON" });
    await user.click(json);
    await user.keyboard("{Control>}a{/Control}{Backspace}");
    await user.paste(JSON.stringify(denyDeletesVersions[0].document));

    expect(screen.getByText("No changes")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Publish v3" })).toBeDisabled();

    // The control: the same paste with one Action changed is a change,
    // and its diff names that line alone.
    await user.keyboard("{Control>}a{/Control}{Backspace}");
    await user.paste(
      JSON.stringify(denyDeletesVersions[0].document).replace(
        "project:delete",
        "project:update",
      ),
    );
    expect(screen.getByText("Edited, not published")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Publish v3" }));
    const diff = within(await screen.findByRole("alertdialog")).getByRole(
      "figure",
    );
    expect(diff).toHaveTextContent("+1 −1");
  });
});
