import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { OrgRole } from "@/app/console/_components/org-role";
import { ReportOrgShell } from "@/app/console/_components/org-shell";
import { OrgSwitcher } from "@/app/console/_components/org-switcher";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";

const route = vi.hoisted(() => ({ pathname: "/console/orgs" }));

vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));

const orgs = [
  { slug: "acme", name: "Acme" },
  { slug: "globex", name: "Globex" },
];

function renderSwitcher(
  pathname: string,
  list: typeof orgs | null = orgs,
  reported?: { org: string; role: OrgRole },
) {
  route.pathname = pathname;
  return render(
    <TooltipProvider>
      <SidebarProvider>
        {reported && <ReportOrgShell {...reported} hidden={[]} />}
        <OrgSwitcher orgs={list} />
      </SidebarProvider>
    </TooltipProvider>,
  );
}

describe("organization switcher", () => {
  it("names the organization the caller is inside", () => {
    renderSwitcher("/console/orgs/globex/projects");

    // Presence: the trigger's name is the sr-only "Switch
    // organization", then the org's name and its slug.
    expect(screen.getByRole("button", { name: /Globex/ })).toBeInTheDocument();
  });

  it("names the caller's role in the organization once it is read", () => {
    renderSwitcher("/console/orgs/globex", orgs, {
      org: "globex",
      role: "admin",
    });

    // Presence, as above: the role follows the org's name.
    expect(
      screen.getByRole("button", { name: /Globex\s*Admin/ }),
    ).toBeInTheDocument();
  });

  it("shows no identifier where the role has not been read", () => {
    renderSwitcher("/console/orgs/globex");

    const trigger = screen.getByRole("button", { name: /Globex/ });
    expect(trigger).toHaveTextContent("G");
    expect(trigger).not.toHaveTextContent("globex");
  });

  it("reads as the organizations list outside an organization", () => {
    renderSwitcher("/console/orgs");

    // Presence: the sr-only "Switch organization" opens the trigger's
    // name, and the neutral heading follows.
    const trigger = screen.getByRole("button", { name: /Organizations/ });
    expect(trigger).not.toHaveTextContent(/select|none/i);
  });

  it("gives each organization its initial and the active one its role", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs/acme", orgs, { org: "acme", role: "owner" });

    await user.click(screen.getByRole("button", { name: /Acme/ }));

    const acme = await screen.findByRole("menuitem", { name: /Acme/ });
    expect(acme).toHaveTextContent(/^AAcmeOwner$/);
    expect(screen.getByRole("menuitem", { name: "Globex" })).toHaveTextContent(
      /^GGlobex$/,
    );
  });

  it("names the organization by its slug when the list was not answered", () => {
    renderSwitcher("/console/orgs/acme", null);

    // Presence: the sr-only label opens the trigger's name.
    const trigger = screen.getByRole("button", {
      name: /Switch organization/,
    });
    expect(trigger).toHaveTextContent("acme");
    expect(trigger).toHaveTextContent("Can’t load organizations");
  });

  it("offers every organization and marks the active one", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs/acme");

    // Presence, as above: the org's name is one part of the trigger's.
    await user.click(screen.getByRole("button", { name: /Acme/ }));

    expect(
      await screen.findByRole("menuitem", { name: "Acme" }),
    ).toHaveAttribute("aria-current", "true");
    const globex = screen.getByRole("menuitem", { name: "Globex" });
    expect(globex).toHaveAttribute("href", "/console/orgs/globex");
    expect(globex).not.toHaveAttribute("aria-current");
  });

  it("offers the create form", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs");

    // Presence, as above: the neutral heading follows the sr-only
    // label.
    await user.click(screen.getByRole("button", { name: /Organizations/ }));

    expect(
      await screen.findByRole("menuitem", { name: "New organization" }),
    ).toHaveAttribute("href", "/console/orgs/new");
  });

  it("still offers the create form to a caller with no organizations", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs", []);

    // Presence, as above: the neutral heading follows the sr-only
    // label.
    await user.click(screen.getByRole("button", { name: /Organizations/ }));

    expect(await screen.findByText("No organizations yet")).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "New organization" }),
    ).toBeInTheDocument();
  });
});
