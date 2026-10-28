import Blockquote from "@tiptap/extension-blockquote";

import type { Plugin } from "../registry";

export const quotePlugin: Plugin = {
  name: "core/quote",
  extensions: [Blockquote],
  blocks: [
    {
      id: "quote",
      node: "blockquote",
      label: "Quote",
      insert: (editor) => editor.chain().focus().toggleBlockquote().run(),
    },
  ],
};
