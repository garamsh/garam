import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import ConsoleLayout from "@/app/console/layout";

// react-dom cannot render the shell's async Server Components under
// Testing Library. A thrown promise stands in for their pending reads
// so the test controls the suspension.
const shell = vi.hoisted(() => ({ pending: undefined as Promise<void> | undefined }));

vi.mock("@/app/console/_components/console-sidebar", () => ({
  ConsoleSidebar: () => {
    if (shell.pending) throw shell.pending;
    return <p>Sidebar</p>;
  },
  ConsoleSidebarFallback: () => <p>Sidebar loading</p>,
}));

vi.mock("@/app/console/_components/console-breadcrumb", () => ({
  ConsoleBreadcrumb: () => {
    if (shell.pending) throw shell.pending;
    return <p>Breadcrumb</p>;
  },
}));

afterEach(() => {
  shell.pending = undefined;
});

describe("console layout", () => {
  it("streams children while the shell's reads are still pending", async () => {
    let resolve!: () => void;
    shell.pending = new Promise<void>((settle) => {
      resolve = settle;
    });

    render(
      <ConsoleLayout>
        <p role="status">Loading organizations…</p>
      </ConsoleLayout>,
    );

    // The `loading.tsx` stand-in: children stream immediately instead
    // of waiting on the sidebar's session and organization reads.
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading organizations…",
    );
    expect(screen.getByText("Sidebar loading")).toBeInTheDocument();
    expect(screen.queryByText("Breadcrumb")).not.toBeInTheDocument();

    await act(async () => {
      shell.pending = undefined;
      resolve();
    });
    expect(screen.getByText("Sidebar")).toBeInTheDocument();
    expect(screen.getByText("Breadcrumb")).toBeInTheDocument();
  });

  it("offers the sidebar trigger on every route", () => {
    render(
      <ConsoleLayout>
        <p>Anything</p>
      </ConsoleLayout>,
    );

    expect(
      screen.getByRole("button", { name: "Toggle Sidebar" }),
    ).toBeInTheDocument();
  });
});
