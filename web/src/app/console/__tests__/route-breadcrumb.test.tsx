import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  ItemCrumb,
  ItemCrumbProvider,
} from "@/app/console/_components/item-crumb";
import { RouteBreadcrumb } from "@/app/console/_components/route-breadcrumb";

const route = vi.hoisted(() => ({ pathname: "/console/orgs" }));

vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));

const orgs = [{ slug: "acme", name: "Acme" }];

function renderBreadcrumb(pathname: string) {
  route.pathname = pathname;
  return render(<RouteBreadcrumb orgs={orgs} />);
}

describe("route breadcrumb", () => {
  it("reads Organizations / Org / Section inside a section", () => {
    renderBreadcrumb("/console/orgs/acme/teams");

    const trail = screen.getByRole("navigation", { name: "breadcrumb" });
    expect(trail).toHaveTextContent("Organizations");
    expect(trail).toHaveTextContent("Acme");
    expect(screen.getByRole("link", { name: "Acme" })).toHaveAttribute(
      "href",
      "/console/orgs/acme",
    );
    expect(screen.getByText("Teams")).toHaveAttribute("aria-current", "page");
  });

  it("follows the route when it changes", () => {
    const { rerender } = renderBreadcrumb("/console/orgs/acme/teams");

    route.pathname = "/console/orgs/acme/permissions";
    rerender(<RouteBreadcrumb orgs={orgs} />);

    expect(screen.getByText("Permissions")).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("names the create form", () => {
    renderBreadcrumb("/console/orgs/new");

    expect(screen.getByText("New organization")).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("names a team with the name its page read", () => {
    route.pathname = "/console/orgs/acme/teams/eng";
    render(
      <ItemCrumbProvider>
        <RouteBreadcrumb orgs={orgs} />
        <ItemCrumb label="Engineering" />
      </ItemCrumbProvider>,
    );

    expect(screen.getByRole("link", { name: "Teams" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/teams",
    );
    expect(screen.getByText("Engineering")).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("does not carry a team's name onto the page navigated to next", () => {
    route.pathname = "/console/orgs/acme/teams/eng";
    const { rerender } = render(
      <ItemCrumbProvider>
        <RouteBreadcrumb orgs={orgs} />
        <ItemCrumb label="Engineering" />
      </ItemCrumbProvider>,
    );
    // The control: the name is there before the navigation.
    expect(screen.getByText("Engineering")).toBeInTheDocument();

    route.pathname = "/console/orgs/acme/teams/ops";
    rerender(
      <ItemCrumbProvider>
        <RouteBreadcrumb orgs={orgs} />
      </ItemCrumbProvider>,
    );

    expect(screen.queryByText("Engineering")).not.toBeInTheDocument();
    expect(screen.getByText("Team")).toHaveAttribute("aria-current", "page");
  });

  it("carries no GRN", () => {
    renderBreadcrumb("/console/orgs/acme/people");

    expect(
      screen.getByRole("navigation", { name: "breadcrumb" }),
    ).not.toHaveTextContent("grn:");
  });
});
