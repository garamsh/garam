import Heading from "@tiptap/extension-heading";

import type { Plugin } from "../registry";

const levels = [1, 2, 3] as const;

export const headingsPlugin: Plugin = {
  name: "core/headings",
  extensions: [Heading.configure({ levels: [...levels] })],
  blocks: levels.map((level) => ({
    id: `heading-${level}`,
    node: "heading",
    label: `Heading ${level}`,
    insert: (editor) => editor.chain().focus().toggleHeading({ level }).run(),
  })),
};
