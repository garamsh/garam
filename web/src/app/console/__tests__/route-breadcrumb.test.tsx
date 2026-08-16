import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

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
    renderBreadcrumb("/console/orgs/acme/groups");

    const trail = screen.getByRole("navigation", { name: "breadcrumb" });
    expect(trail).toHaveTextContent("Organizations");
    expect(trail).toHaveTextContent("Acme");
    expect(screen.getByRole("link", { name: "Acme" })).toHaveAttribute(
      "href",
      "/console/orgs/acme",
    );
    expect(screen.getByText("Groups")).toHaveAttribute("aria-current", "page");
  });

  it("follows the route when it changes", () => {
    const { rerender } = renderBreadcrumb("/console/orgs/acme/groups");

    route.pathname = "/console/orgs/acme/policies";
    rerender(<RouteBreadcrumb orgs={orgs} />);

    expect(screen.getByText("Policies")).toHaveAttribute(
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

  it("carries no GRN", () => {
    renderBreadcrumb("/console/orgs/acme/members");

    expect(
      screen.getByRole("navigation", { name: "breadcrumb" }),
    ).not.toHaveTextContent("grn:");
  });
});
