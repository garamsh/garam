import Link from "@tiptap/extension-link";

import type { Plugin } from "../registry";

/**
 * Links in a page's text. A pasted URL becomes one, so a page links a
 * message by its address, `/<org>/channels/<channel>?thread=<id>`, as
 * it links anything else. The plugin adds a mark and no block.
 */
export const linkPlugin: Plugin = {
  name: "core/link",
  extensions: [
    Link.configure({ autolink: true, linkOnPaste: true, openOnClick: true }),
  ],
};
