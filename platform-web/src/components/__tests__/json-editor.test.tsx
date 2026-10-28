import { render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { JsonEditor } from "@/components/json-editor";

const setProblem = vi.fn();

vi.mock("@/lib/codemirror", () => ({
  mountJsonEditor: () => ({
    setDoc: () => {},
    setAttributes: () => {},
    setProblem,
    destroy: () => {},
  }),
}));

describe("JsonEditor", () => {
  it("marks a problem once, and again only when what it says changes", async () => {
    const { container, rerender } = render(
      <JsonEditor value="{}" label="JSON" problem={{ message: "Bad." }} />,
    );
    await waitFor(() =>
      expect(setProblem).toHaveBeenLastCalledWith({
        message: "Bad.",
        path: undefined,
      }),
    );
    // Mounted: the editor has replaced the server-rendered text.
    await waitFor(() =>
      expect(
        container.querySelector('[data-slot="json-editor"]'),
      ).toHaveAttribute("aria-busy", "false"),
    );

    // An equal problem in a fresh object, as every keystroke renders.
    // The rerender flushes any effect the mount left pending, so the
    // count is what the mount and this render marked between them.
    rerender(
      <JsonEditor value="{ }" label="JSON" problem={{ message: "Bad." }} />,
    );
    expect(setProblem).toHaveBeenCalledTimes(1);

    // The control: a problem that says something else is marked.
    rerender(
      <JsonEditor
        value="{ }"
        label="JSON"
        problem={{ message: "Worse.", path: "/Version" }}
      />,
    );
    expect(setProblem).toHaveBeenLastCalledWith({
      message: "Worse.",
      path: "/Version",
    });
  });
});
