import CodeBlock from "@tiptap/extension-code-block";

import type { Plugin } from "../registry";

export const codePlugin: Plugin = {
  name: "core/code",
  extensions: [CodeBlock],
  blocks: [
    {
      id: "code",
      node: "codeBlock",
      label: "Code",
      insert: (editor) => editor.chain().focus().toggleCodeBlock().run(),
    },
  ],
};
