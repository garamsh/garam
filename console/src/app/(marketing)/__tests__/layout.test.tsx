import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import MarketingLayout from "@/app/(marketing)/layout";

describe("marketing layout", () => {
  it("navigates to the landing sections and the way in", () => {
    render(<MarketingLayout>{null}</MarketingLayout>);

    const nav = screen.getByRole("navigation", { name: "Sections" });
    expect(
      within(nav).getByRole("link", { name: "Organization" }),
    ).toHaveAttribute("href", "#organization");
    expect(within(nav).getByRole("link", { name: "Agents" })).toHaveAttribute(
      "href",
      "#agents",
    );
    expect(
      within(nav).getByRole("link", { name: "Identifiers" }),
    ).toHaveAttribute("href", "#identifiers");

    const header = screen.getByRole("banner");
    expect(
      within(header).getByRole("link", { name: "Open the console" }),
    ).toHaveAttribute("href", "/console");
    expect(
      within(header).getByRole("link", { name: "Sign in" }),
    ).toHaveAttribute("href", "/login");
  });

  it("repeats the product and section links in the footer", () => {
    render(<MarketingLayout>{null}</MarketingLayout>);

    const footer = screen.getByRole("contentinfo");
    expect(
      within(footer).getByRole("link", { name: "Open the console" }),
    ).toHaveAttribute("href", "/console");
    expect(
      within(footer).getByRole("link", { name: "Sign in" }),
    ).toHaveAttribute("href", "/login");
    expect(
      within(footer).getByRole("link", { name: "Identifiers" }),
    ).toHaveAttribute("href", "#identifiers");
  });
});
