import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { OrgSwitcher } from "@/app/console/_components/org-switcher";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";

const route = vi.hoisted(() => ({ pathname: "/console/orgs" }));

vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));

const orgs = [
  { slug: "acme", name: "Acme" },
  { slug: "globex", name: "Globex" },
];

function renderSwitcher(pathname: string, list = orgs) {
  route.pathname = pathname;
  return render(
    <TooltipProvider>
      <SidebarProvider>
        <OrgSwitcher orgs={list} />
      </SidebarProvider>
    </TooltipProvider>,
  );
}

describe("organization switcher", () => {
  it("names the organization the caller is inside", () => {
    renderSwitcher("/console/orgs/globex/projects");

    expect(
      screen.getByRole("button", { name: /Globex/ }),
    ).toBeInTheDocument();
  });

  it("says so when no organization is selected", () => {
    renderSwitcher("/console/orgs");

    expect(
      screen.getByRole("button", { name: /Select organization/ }),
    ).toBeInTheDocument();
  });

  it("offers every organization and marks the active one", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs/acme");

    await user.click(screen.getByRole("button", { name: /Acme/ }));

    expect(await screen.findByRole("menuitem", { name: "Acme" })).toHaveAttribute(
      "aria-current",
      "true",
    );
    const globex = screen.getByRole("menuitem", { name: "Globex" });
    expect(globex).toHaveAttribute("href", "/console/orgs/globex");
    expect(globex).not.toHaveAttribute("aria-current");
  });

  it("offers the create form", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs");

    await user.click(screen.getByRole("button", { name: /Select organization/ }));

    expect(
      await screen.findByRole("menuitem", { name: "New organization" }),
    ).toHaveAttribute("href", "/console/orgs/new");
  });

  it("still offers the create form to a caller with no organizations", async () => {
    const user = userEvent.setup();
    renderSwitcher("/console/orgs", []);

    await user.click(screen.getByRole("button", { name: /Select organization/ }));

    expect(await screen.findByText("No organizations yet")).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "New organization" }),
    ).toBeInTheDocument();
  });
});
