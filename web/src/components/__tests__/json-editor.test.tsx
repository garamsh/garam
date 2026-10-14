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
    const { rerender } = render(
      <JsonEditor value="{}" label="JSON" problem={{ message: "Bad." }} />,
    );
    await waitFor(() =>
      expect(setProblem).toHaveBeenLastCalledWith({
        message: "Bad.",
        path: undefined,
      }),
    );
    const settled = setProblem.mock.calls.length;

    // An equal problem in a fresh object, as every keystroke renders.
    rerender(
      <JsonEditor value="{ }" label="JSON" problem={{ message: "Bad." }} />,
    );
    expect(setProblem).toHaveBeenCalledTimes(settled);

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
