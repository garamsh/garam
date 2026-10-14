import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PolicyPage from "@/app/console/orgs/[org]/permissions/[policy]/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  baselineRead,
  denyDeletes,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const POLICIES = `${API_URL}/orgs/acme/policies`;
const DENY = `${POLICIES}/d1e2f3a4b5c6d7e8`;

// The console layout mounts the provider a `<Time>` needs.
function renderPolicy(policy = "d1e2f3a4b5c6d7e8") {
  return renderServerComponent(
    PolicyPage({ params: Promise.resolve({ org: "acme", policy }) }),
    { wrapper: TooltipProvider },
  );
}

async function openTab(name: RegExp) {
  await userEvent.setup().click(screen.getByRole("tab", { name }));
}

describe("policy page", () => {
  it("heads the page with the name, status and identifier, and opens on the document", async () => {
    const { container } = await renderPolicy();

    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: "Policy identifier" }),
    ).toHaveTextContent("d1e2f3a4b5c6d7e8");
    expect(
      screen.getByText(
        "Active: version 1 takes part in every check it reaches.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Applies to Engineering (Team)."),
    ).toBeInTheDocument();
    // The editor opens on the default version, v1, and nothing to
    // publish.
    expect(
      screen.getByRole("textbox", { name: "Statement 1 action 1" }),
    ).toHaveValue("project:delete");
    expect(screen.getByRole("button", { name: "Publish v3" })).toBeDisabled();
    // Archiving is in the danger zone, at the end of the last tab.
    expect(container.textContent).not.toContain("grn:acme:default:group");
  });

  it("lists the versions with the default marked, and whom it is attached to", async () => {
    await renderPolicy();

    // Presence: a tab's name carries its count.
    await openTab(/^Versions/);
    const current = screen.getByRole("row", { name: /^v1/ });
    expect(within(current).getByText("Default · in use")).toBeInTheDocument();
    const newer = screen.getByRole("row", { name: /^v2/ });
    expect(
      within(newer).getByRole("button", { name: "Make v2 default" }),
    ).toBeInTheDocument();

    await openTab(/^Attachments/);
    // Presence: a row's accessible name is all of its cells.
    const row = screen.getByRole("row", { name: /Engineering/ });
    expect(within(row).getByText("Team")).toBeInTheDocument();
    expect(
      within(row).getByRole("button", {
        name: "Detach Deny project deletion from Engineering",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Archive policy" }),
    ).toBeInTheDocument();
  });

  it("says a draft grants nothing, whom it would reach once active, and offers activation", async () => {
    server.use(
      http.get(POLICIES, () =>
        HttpResponse.json([{ ...denyDeletes, status: "draft" }]),
      ),
      http.get(`${DENY}/attachments`, () => HttpResponse.json([])),
    );

    await renderPolicy();

    expect(
      screen.getByText("Draft: grants no access until it is activated."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Not evaluated while draft. Once active: No attachments: applies to everyone in this organization.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Activate" }),
    ).toBeInTheDocument();
  });

  it("warns that attaching narrows an active policy that reaches everyone", async () => {
    server.use(http.get(POLICIES, () => HttpResponse.json([baselineRead])));

    await renderPolicy("0a1b2c3d4e5f6071");

    expect(
      screen.getByText(
        "No attachments: applies to everyone in this organization.",
      ),
    ).toBeInTheDocument();
    // Presence: the warning is a sentence that goes on to say how.
    expect(screen.getByText(/^Attaching narrows it/)).toBeInTheDocument();
  });

  it("does not call refused attachments no attachments", async () => {
    // The control: listed as empty, the policy says it has none.
    server.use(http.get(`${DENY}/attachments`, () => HttpResponse.json([])));
    const listed = await renderPolicy();
    await openTab(/^Attachments/);
    expect(screen.getByText("No attachments")).toBeInTheDocument();
    listed.unmount();

    server.use(
      http.get(`${DENY}/attachments`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderPolicy();
    // Presence: the reach is a sentence that goes on to say why. The
    // Attachments panel is mounted too and says it again.
    expect(
      within(screen.getByRole("tabpanel", { name: "Document" })).getByText(
        /may apply to everyone in this organization\.$/,
      ),
    ).toBeInTheDocument();
    await openTab(/^Attachments/);

    expect(screen.getByText("Attachments not shown")).toBeInTheDocument();
    expect(screen.queryByText("No attachments")).not.toBeInTheDocument();
  });

  it("withdraws only the writes the server denies, and keeps the document readable", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "policy:attach" ? "allow" : "deny",
      ),
    );

    await renderPolicy();

    // Denied a change, the document is shown and not offered for edit.
    expect(
      screen.queryByRole("button", { name: /^Publish/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("Your access lets you read this policy, not change it."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Statement 1 action 1" }),
    ).toHaveAttribute("readonly");

    await openTab(/^Versions/);
    expect(
      screen.queryByRole("button", { name: "Make v2 default" }),
    ).not.toBeInTheDocument();

    await openTab(/^Attachments/);
    expect(
      screen.queryByRole("button", { name: /^Detach/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Archive policy" }),
    ).not.toBeInTheDocument();
    // The control: attaching, allowed, is still offered.
    expect(
      screen.getByRole("combobox", { name: "Team, person or agent" }),
    ).toBeInTheDocument();
  });

  it("offers every write when the check could not be asked", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.error(),
      ),
    );

    await renderPolicy();

    expect(
      screen.getByRole("button", { name: "Publish v3" }),
    ).toBeInTheDocument();
    await openTab(/^Attachments/);
    expect(
      screen.getByRole("button", {
        name: "Detach Deny project deletion from Engineering",
      }),
    ).toBeInTheDocument();
  });

  it("renders not-found for a policy the organization's list does not hold", async () => {
    // The control: a policy the list holds renders under its name.
    const listed = await renderPolicy();
    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    listed.unmount();

    // garam answers an empty list of versions and attachments for any
    // identifier, so only the policy list can say it does not exist.
    server.use(
      http.get(`${POLICIES}/nope/versions`, () => HttpResponse.json([])),
      http.get(`${POLICIES}/nope/attachments`, () => HttpResponse.json([])),
    );

    await expect(renderPolicy("nope")).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("withdraws Activate where the server denies changing the policy", async () => {
    server.use(
      http.get(POLICIES, () =>
        HttpResponse.json([{ ...denyDeletes, status: "draft" }]),
      ),
    );
    // The control: allowed, a draft offers Activate.
    const allowed = await renderPolicy();
    expect(
      screen.getByRole("button", { name: "Activate" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "policy:update" ? "deny" : "allow",
      ),
    );
    await renderPolicy();

    expect(
      screen.queryByRole("button", { name: "Activate" }),
    ).not.toBeInTheDocument();
    // The page still reads: the draft's state is said.
    expect(
      screen.getByText("Draft: grants no access until it is activated."),
    ).toBeInTheDocument();
  });

  it("says an activation landed beside the header once its button is gone", async () => {
    server.use(
      http.get(POLICIES, () =>
        HttpResponse.json([{ ...denyDeletes, status: "draft" }]),
      ),
      http.post(`${DENY}/activate`, () => HttpResponse.json(denyDeletes)),
    );
    const user = userEvent.setup();
    await renderPolicy();

    await user.click(screen.getByRole("button", { name: "Activate" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Activate policy" }),
    );

    expect(
      await screen.findByText("Activated: v1 takes part in checks."),
    ).toBeInTheDocument();
  });
});
