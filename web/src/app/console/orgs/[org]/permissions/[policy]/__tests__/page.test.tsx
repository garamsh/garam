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

describe("policy page", () => {
  it("heads the page with the name, status and identifier, and opens on the document", async () => {
    const { container } = await renderPolicy();

    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "d1e2f3a4b5c6d7e8",
    );
    // The status is the header's; the document says whom it reaches.
    const document = panelOf(/^Document/);
    expect(
      document.getByText("Applies to Engineering (Team)."),
    ).toBeInTheDocument();
    expect(document.queryByText(/^Active/)).not.toBeInTheDocument();
    // The editor opens on the default version, v1, and nothing to
    // publish.
    expect(
      document.getByRole("textbox", { name: "Statement 1 action 1" }),
    ).toHaveValue("project:delete");
    expect(document.getByRole("button", { name: "Publish v3" })).toBeDisabled();
    // Archiving is in the danger zone, at the end of the last tab.
    expect(container.textContent).not.toContain("grn:acme:default:group");
  });

  it("lists the versions with the default marked, and whom it is attached to", async () => {
    await renderPolicy();

    // Presence: a tab's name carries its count.
    const versions = await openTab(/^Versions/);
    const current = versions.getByRole("row", { name: /^v1/ });
    expect(within(current).getByText("In use")).toBeInTheDocument();
    const newer = versions.getByRole("row", { name: /^v2/ });
    expect(
      within(newer).getByRole("button", { name: "Make v2 default" }),
    ).toBeInTheDocument();

    const attachments = await openTab(/^Attachments/);
    // Presence: a row's accessible name is all of its cells.
    const row = attachments.getByRole("row", { name: /Engineering/ });
    expect(within(row).getByText("Team")).toBeInTheDocument();
    expect(
      within(row).getByRole("button", {
        name: "Detach Deny project deletion from Engineering",
      }),
    ).toBeInTheDocument();
    expect(
      attachments.getByRole("button", { name: "Archive policy" }),
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

    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(
      panelOf(/^Document/).getByText(
        "Once active: Applies to everyone: it has no attachments.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Activate" }),
    ).toBeInTheDocument();
  });

  it("warns that an active policy with no attachment reaches everyone", async () => {
    server.use(http.get(POLICIES, () => HttpResponse.json([baselineRead])));

    await renderPolicy("0a1b2c3d4e5f6071");

    const warning = panelOf(/^Document/)
      .getByText("Applies to everyone: it has no attachments.")
      .closest("[data-tone]");
    expect(warning).toHaveAttribute("data-tone", "warn");
  });

  it("does not call refused attachments no attachments", async () => {
    // The control: listed as empty, the policy says it has none.
    server.use(http.get(`${DENY}/attachments`, () => HttpResponse.json([])));
    const listed = await renderPolicy();
    expect(
      (await openTab(/^Attachments/)).getByText("Applies to everyone."),
    ).toBeInTheDocument();
    listed.unmount();

    server.use(
      http.get(`${DENY}/attachments`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderPolicy();
    expect(
      panelOf(/^Document/).getByText(
        "May apply to everyone: its attachments are not listed to you.",
      ),
    ).toBeInTheDocument();
    const attachments = await openTab(/^Attachments/);

    expect(
      attachments.getByText("You can't view this policy's attachments"),
    ).toBeInTheDocument();
    expect(
      attachments.queryByText("Applies to everyone."),
    ).not.toBeInTheDocument();
  });

  it("withdraws only the writes the server denies, and keeps the document readable", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "policy:attach" ? "allow" : "deny",
      ),
    );

    await renderPolicy();

    // Denied a change, the document is shown and not offered for edit.
    const document = panelOf(/^Document/);
    expect(
      document.queryByRole("button", { name: /^Publish/ }),
    ).not.toBeInTheDocument();
    expect(document.getByText("Read-only for you.")).toBeInTheDocument();
    expect(
      document.getByRole("textbox", { name: "Statement 1 action 1" }),
    ).toHaveAttribute("readonly");

    const versions = await openTab(/^Versions/);
    expect(
      versions.queryByRole("button", { name: "Make v2 default" }),
    ).not.toBeInTheDocument();

    const attachments = await openTab(/^Attachments/);
    expect(
      attachments.queryByRole("button", { name: /^Detach/ }),
    ).not.toBeInTheDocument();
    expect(
      attachments.queryByRole("button", { name: "Archive policy" }),
    ).not.toBeInTheDocument();
    // The control: attaching, allowed, is still offered.
    expect(
      attachments.getByRole("combobox", { name: "Attach to" }),
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
      panelOf(/^Document/).getByRole("button", { name: "Publish v3" }),
    ).toBeInTheDocument();
    expect(
      (await openTab(/^Attachments/)).getByRole("button", {
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
    // The page still reads: the draft's state and reach are said.
    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(panelOf(/^Document/).getByText(/^Once active:/)).toBeInTheDocument();
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
    const activate = screen.getByRole("button", { name: "Activate" });
    // The header: the smallest element holding the policy's name and the
    // control that activates it. The wait below reads only this, so each
    // of its checks stays cheap on a page this large.
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
