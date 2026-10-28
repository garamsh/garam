import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Page } from "@/lib/cowork";

import { PageTree } from "../page-tree";

const page = (
  id: string,
  title: string,
  parent: string,
  position: number,
): Page => ({
  id,
  grn: `grn:acme:default:cowork-page:${id}`,
  org: "acme",
  parent,
  title,
  position,
  createdBy: "grn:root:default:user:ada",
  createdAt: "2026-10-27T00:00:00Z",
  updatedAt: "2026-10-27T00:00:00Z",
});

describe("PageTree", () => {
  it("nests children under their parent and orders siblings by position", () => {
    render(
      <PageTree
        org="acme"
        current="c"
        pages={[
          page("b", "Second", "", 1),
          page("a", "First", "", 0),
          page("c", "Child", "a", 0),
        ]}
      />,
    );
    const links = screen.getAllByRole("link").map((l) => l.textContent);
    expect(links).toEqual(["First", "Child", "Second"]);
    const first = screen.getByRole("link", { name: "First" }).closest("li")!;
    expect(within(first).getByRole("link", { name: "Child" })).toHaveAttribute(
      "href",
      "/acme/c",
    );
    expect(screen.getByRole("link", { name: "Child" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("leaves out a page whose parent it was not given", () => {
    render(
      <PageTree
        org="acme"
        pages={[page("c", "Orphan", "hidden", 0), page("a", "Shown", "", 0)]}
      />,
    );
    expect(
      screen.queryByRole("link", { name: "Orphan" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Shown" })).toBeInTheDocument();
  });

  it("says so when there are no pages", () => {
    render(<PageTree org="acme" pages={[]} />);
    expect(screen.getByText("No pages yet.")).toBeInTheDocument();
  });
});
