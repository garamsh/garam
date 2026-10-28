import { fireEvent, render, screen } from "@testing-library/react";
import type { Editor } from "@tiptap/react";
import { describe, expect, it, vi } from "vitest";

import { BlockToolbar } from "../block-toolbar";

describe("BlockToolbar", () => {
  const editor = {} as Editor;

  it("inserts the block it names into the editor", () => {
    const insert = vi.fn();
    render(
      <BlockToolbar
        blocks={[
          { id: "heading-1", node: "heading", label: "Heading 1", insert },
        ]}
        editor={editor}
        disabled={false}
        status="connected"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Heading 1" }));
    expect(insert).toHaveBeenCalledWith(editor);
  });

  // A button that takes focus on press leaves the editor until the
  // block's insert hands focus back, which Tiptap does a frame later:
  // whatever is typed in that frame reaches the button and is lost.
  it("keeps focus in the editor when a button is pressed", () => {
    render(
      <BlockToolbar
        blocks={[
          {
            id: "heading-1",
            node: "heading",
            label: "Heading 1",
            insert: vi.fn(),
          },
        ]}
        editor={editor}
        disabled={false}
        status="connected"
      />,
    );
    const notCancelled = fireEvent.mouseDown(
      screen.getByRole("button", { name: "Heading 1" }),
    );
    expect(notCancelled).toBe(false);
  });
});
