import Bold from "@tiptap/extension-bold";
import Code from "@tiptap/extension-code";
import Document from "@tiptap/extension-document";
import Italic from "@tiptap/extension-italic";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";

import type { Plugin } from "../registry";

/** The page itself, plain paragraphs, and the marks text carries. */
export const textPlugin: Plugin = {
  name: "core/text",
  extensions: [Document, Text, Paragraph, Bold, Italic, Code],
  blocks: [
    {
      id: "paragraph",
      node: "paragraph",
      label: "Text",
      insert: (editor) => editor.chain().focus().setParagraph().run(),
    },
  ],
};
