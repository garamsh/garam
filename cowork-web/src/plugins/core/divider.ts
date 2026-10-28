import HorizontalRule from "@tiptap/extension-horizontal-rule";

import type { Plugin } from "../registry";

export const dividerPlugin: Plugin = {
  name: "core/divider",
  extensions: [HorizontalRule],
  blocks: [
    {
      id: "divider",
      node: "horizontalRule",
      label: "Divider",
      insert: (editor) => editor.chain().focus().setHorizontalRule().run(),
    },
  ],
};
