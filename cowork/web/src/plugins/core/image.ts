import Image from "@tiptap/extension-image";

import type { Plugin } from "../registry";

/**
 * An image by URL. Uploading one into cowork's blob store is #1667's;
 * until then the block names an image served elsewhere.
 */
export const imagePlugin: Plugin = {
  name: "core/image",
  extensions: [Image],
  blocks: [
    {
      id: "image",
      node: "image",
      label: "Image",
      insert: (editor) => {
        const src = window.prompt("Image URL");
        if (src) editor.chain().focus().setImage({ src }).run();
      },
    },
  ],
};
