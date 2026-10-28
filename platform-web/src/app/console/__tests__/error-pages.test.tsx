import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import {
  DetailPageSkeleton,
  ListPageSkeleton,
} from "@/app/console/_components/page-skeletons";
import ConsoleError from "@/app/console/error";
import ConsoleNotFound from "@/app/console/not-found";

describe("console error boundary", () => {
  it("says what happened in words and never the thrown message", () => {
    const error = Object.assign(
      new Error("org not found: grn:root:default:org:acme"),
      { digest: "1234567890" },
    );
    render(<ConsoleError error={error} reset={() => {}} />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "This page didn’t load",
    );
    expect(screen.queryByText(/org not found/)).not.toBeInTheDocument();
    // The digest matches the server log, so it is the reference
    // offered.
    expect(screen.getByText("1234567890")).toBeInTheDocument();
  });

  it("tries the segment again", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<ConsoleError error={new Error("x")} reset={reset} />);

    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(reset).toHaveBeenCalledOnce();
    expect(
      screen.getByRole("link", { name: "Your organizations" }),
    ).toHaveAttribute("href", "/console/orgs");
  });
});

describe("console not-found page", () => {
  it("covers a page that does not exist and one the caller cannot see", () => {
    render(<ConsoleNotFound />);

    expect(
      screen.getByRole("heading", { name: "Page not found" }),
    ).toBeInTheDocument();
    // Presence: the sentence names both cases ADR-0011 answers alike.
    expect(
      screen.getByText(/doesn’t exist, or you don’t have access/),
    ).toBeInTheDocument();
  });
});

describe("page skeletons", () => {
  it("announce the page as loading", () => {
    render(
      <>
        <ListPageSkeleton />
        <DetailPageSkeleton />
      </>,
    );

    expect(screen.getAllByRole("status", { name: "Loading" })).toHaveLength(2);
  });
});
