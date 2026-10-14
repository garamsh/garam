import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { KeysPanel } from "@/app/console/orgs/[org]/operators/[operator]/_components/keys-panel";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { OperatorKey } from "@/features/orgs";
import { API_URL, fieldOneKey } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` needs a request scope the test does not run in.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const retiring: OperatorKey = {
  ...fieldOneKey,
  kid: "Ret1ringKeyFingerprint000000000000000000000",
  state: "retiring",
  retiresAt: "2026-10-15T09:00:00Z",
};
const revoked: OperatorKey = {
  ...fieldOneKey,
  kid: "Rev0kedKeyFingerprint000000000000000000000",
  state: "revoked",
  revokedAt: "2026-10-02T09:00:00Z",
  lastExchangedAt: undefined,
};

/** Records the keys the panel revoked, and answers as garam does. */
function recordRevocations() {
  const revokedPaths: string[] = [];
  server.use(
    http.delete(
      `${API_URL}/orgs/acme/operators/field-one/keys/:kid`,
      ({ request }) => {
        revokedPaths.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      },
    ),
  );
  return revokedPaths;
}

function renderPanel(
  keys: OperatorKey[],
  { canIssue = true, canRevoke = true } = {},
) {
  return render(
    <TooltipProvider>
      <KeysPanel
        org="acme"
        operator="field-one"
        keys={keys}
        setupHref="/console/orgs/acme/operators/field-one/connect"
        canIssue={canIssue}
        canRevoke={canRevoke}
      />
    </TooltipProvider>,
  );
}

function rows() {
  return within(screen.getByRole("table", { name: "Keys" }))
    .getAllByRole("row")
    .slice(1);
}

describe("operator keys", () => {
  it("says where each key stands, and when it last signed in or never", () => {
    renderPanel([revoked, retiring, fieldOneKey]);

    const [first, second, third] = rows();
    expect(first).toHaveTextContent("Revoked");
    expect(first).toHaveTextContent("Never");
    expect(second).toHaveTextContent("Retiring");
    expect(second).toHaveTextContent("until");
    expect(third).toHaveTextContent("Active");
    // A fingerprint is shortened in the table and copied whole.
    expect(third).toHaveTextContent(`${fieldOneKey.kid.slice(0, 12)}…`);
    expect(
      within(third).getByRole("button", { name: "Copy fingerprint" }),
    ).toBeInTheDocument();
  });

  it("offers revoking a live key and not a revoked one", () => {
    renderPanel([revoked, fieldOneKey]);

    const [revokedRow, activeRow] = rows();
    expect(
      within(revokedRow).queryByRole("button", { name: "Revoke" }),
    ).toBeNull();
    expect(
      within(activeRow).getByRole("button", { name: "Revoke" }),
    ).toBeInTheDocument();
  });

  it("revokes only once confirmed, naming the key to garam", async () => {
    const revokedPaths = recordRevocations();
    const user = userEvent.setup();
    renderPanel([fieldOneKey]);

    await user.click(screen.getByRole("button", { name: "Revoke" }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Revoke this key?",
    });
    // Presence: the sentence goes on to say the record stays.
    expect(dialog).toHaveTextContent(/every token exchanged under it/);
    expect(revokedPaths).toEqual([]);

    await user.click(
      within(dialog).getByRole("button", { name: "Revoke key" }),
    );

    expect(await screen.findByRole("status")).toHaveTextContent("Revoked");
    expect(revokedPaths).toEqual([
      `/orgs/acme/operators/field-one/keys/${fieldOneKey.kid}`,
    ]);
  });

  it("sends nothing when revoking is cancelled", async () => {
    const revokedPaths = recordRevocations();
    const user = userEvent.setup();
    renderPanel([fieldOneKey]);

    await user.click(screen.getByRole("button", { name: "Revoke" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(revokedPaths).toEqual([]);
  });

  it("offers issuing to an operator with no live key, and rotating to one with a key", () => {
    const { unmount } = renderPanel([revoked]);
    expect(screen.getByRole("link", { name: "Issue a key" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/operators/field-one/connect",
    );
    unmount();

    renderPanel([fieldOneKey]);
    expect(screen.getByRole("link", { name: "Rotate key" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/operators/field-one/connect?rotate",
    );
  });

  it("offers neither where the server denies them", () => {
    renderPanel([fieldOneKey], { canIssue: false, canRevoke: false });

    expect(screen.queryByRole("link", { name: "Rotate key" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Revoke" })).toBeNull();
    // Control: the key is still listed.
    expect(rows()).toHaveLength(1);
  });

  it("names the absence when no key was ever issued", () => {
    renderPanel([]);

    expect(screen.getByText("No keys issued")).toBeInTheDocument();
  });
});
