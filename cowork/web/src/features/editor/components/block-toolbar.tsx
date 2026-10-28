import type { Editor } from "@tiptap/react";

import type { BlockType } from "@/plugins/registry";

type Props = {
  blocks: readonly BlockType[];
  editor: Editor | null;
  disabled: boolean;
  /** What the sync status reads, beside the buttons. */
  status: string;
};

/** The insert menu: one button per block type, and the sync status. */
export function BlockToolbar({ blocks, editor, disabled, status }: Props) {
  return (
    <div
      className="flex flex-wrap items-center gap-2"
      role="toolbar"
      aria-label="Insert a block"
    >
      {blocks.map((block) => (
        <button
          key={block.id}
          type="button"
          className="rounded border border-neutral-300 px-2 py-1 text-sm hover:bg-neutral-100 disabled:opacity-50"
          disabled={!editor || disabled}
          // Pressing the button leaves focus in the editor: an insert's
          // focus() returns it only a frame later, and keys typed in
          // that frame would reach the button.
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => editor && block.insert(editor)}
        >
          {block.label}
        </button>
      ))}
      <span
        className="ml-auto text-xs text-neutral-500"
        data-testid="sync-status"
      >
        {status}
      </span>
    </div>
  );
}
