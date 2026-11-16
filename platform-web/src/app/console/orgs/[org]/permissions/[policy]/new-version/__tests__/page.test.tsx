import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import NewVersionPage from "@/app/console/orgs/[org]/permissions/[policy]/new-version/page";
import {
  DENY_DELETES_ID,
  ENGINEERING_ADMINS_ID,
  answerPermissionChecks,
} from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () =>
    `/console/orgs/acme/permissions/${DENY_DELETES_ID}/new-version`,
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function renderNewVersion(policy: string = DENY_DELETES_ID) {
  return renderServerComponent(
    NewVersionPage({ params: Promise.resolve({ org: "acme", policy }) }),
  );
}

describe("new version page", () => {
  it("starts the editor from the default version's text", async () => {
    await renderNewVersion();

    expect(screen.getByLabelText("Cedar text")).toHaveValue(
      'forbid (principal, action == Action::"iam:project.delete", resource);',
    );
    expect(
      screen.getByText(/It is stored and not the default/),
    ).toBeInTheDocument();
  });

  it("stores the text and says it is not the default yet", async () => {
    const user = userEvent.setup();
    await renderNewVersion();
    const text = screen.getByLabelText("Cedar text");
    await user.clear(text);
    await user.type(text, "forbid (principal, action, resource);");
    // The control: nothing is said before the version is stored.
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Store version" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Stored as v3. It is not the default yet",
    );
    expect(
      screen.getByRole("link", { name: "Back to the policy" }),
    ).toHaveAttribute(
      "href",
      `/console/orgs/acme/permissions/${DENY_DELETES_ID}`,
    );
  });

  it("says the service's message at the editor and keeps the text", async () => {
    const user = userEvent.setup();
    await renderNewVersion();
    const text = screen.getByLabelText("Cedar text");
    await user.clear(text);
    await user.type(text, "not cedar");
    expect(text).not.toHaveAttribute("aria-invalid", "true");

    await user.click(screen.getByRole("button", { name: "Store version" }));

    await waitFor(() => expect(text).toHaveAttribute("aria-invalid", "true"));
    expect(text).toHaveAccessibleDescription(
      expect.stringContaining("unexpected token at line 1"),
    );
    expect(text).toHaveValue("not cedar");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("offers a role link no form, and says why", async () => {
    // The control: a guardrail's page offers the editor.
    const guardrail = await renderNewVersion();
    expect(screen.getByLabelText("Cedar text")).toBeInTheDocument();
    guardrail.unmount();

    await renderNewVersion(ENGINEERING_ADMINS_ID);

    expect(screen.getByRole("status")).toHaveTextContent(
      "A role link takes no new version",
    );
    expect(screen.queryByLabelText("Cedar text")).not.toBeInTheDocument();
  });

  it("withdraws the form from a caller the server denies", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "iam:policy.update" ? "deny" : "allow",
      ),
    );

    await renderNewVersion();

    expect(screen.getByRole("status")).toHaveTextContent(
      "No policy lets you change this policy",
    );
    expect(screen.queryByLabelText("Cedar text")).not.toBeInTheDocument();
  });
});
