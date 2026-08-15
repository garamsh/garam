import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import AppLayout from "@/app/console/layout";

// react-dom cannot render the real UserMenu, an async Server
// Component, under Testing Library. A thrown promise stands in for the
// pending `/me` fetch so the test controls the suspension.
const session = vi.hoisted(() => ({
  pending: undefined as Promise<void> | undefined,
}));

vi.mock("@/features/auth/components/user-menu", () => ({
  UserMenu: () => {
    if (session.pending) throw session.pending;
    return <span>Alice</span>;
  },
}));

afterEach(() => {
  session.pending = undefined;
});

describe("console layout", () => {
  it("renders the shell and children while the session section is pending", async () => {
    let resolve!: () => void;
    session.pending = new Promise<void>((r) => {
      resolve = r;
    });

    render(
      <AppLayout>
        <p role="status">Loading organizations…</p>
      </AppLayout>,
    );

    // The `loading.tsx` stand-in: children stream immediately instead
    // of waiting on the header's session fetch.
    expect(screen.getByRole("link", { name: "garam" })).toHaveAttribute(
      "href",
      "/console/orgs",
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading organizations…",
    );
    expect(screen.queryByText("Alice")).not.toBeInTheDocument();

    await act(async () => {
      session.pending = undefined;
      resolve();
    });
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });
});
