import type { AnyExtension, Editor } from "@tiptap/core";

/**
 * One kind of block a page holds, as the insert menu offers it. `node`
 * is the name the document stores the block under, which cowork's
 * server reads back (ADR-0127): `paragraph`, `heading`, `taskList` ...
 * Several entries may insert the same node, as the three heading
 * levels do; `id` tells them apart.
 */
export type BlockType = {
  id: string;
  node: string;
  label: string;
  /** Turns the current block into this one, or adds it at the selection. */
  insert: (editor: Editor) => void;
};

/**
 * What a plugin adds to the editor. Blocks are the first thing a plugin
 * registers; views, commands and side panels join this type with the
 * milestones that build them (ADR-0127 §Decision 9).
 */
export type Plugin = {
  name: string;
  /** The TipTap extensions the plugin's blocks and marks need. */
  extensions: AnyExtension[];
  blocks?: BlockType[];
};

export type Registry = {
  register(plugin: Plugin): void;
  plugins(): readonly Plugin[];
  blocks(): readonly BlockType[];
  /** Every registered extension, each once, in registration order. */
  extensions(): AnyExtension[];
};

/**
 * A registry with nothing in it. A plugin name or block ID registered
 * twice is a bug in the manifest that assembles them, so it throws.
 */
export function createRegistry(): Registry {
  const plugins: Plugin[] = [];
  return {
    register(plugin) {
      if (plugins.some((p) => p.name === plugin.name)) {
        throw new Error(`plugin ${plugin.name} is registered twice`);
      }
      const ids = new Set(
        plugins.flatMap((p) => p.blocks ?? []).map((b) => b.id),
      );
      for (const block of plugin.blocks ?? []) {
        if (ids.has(block.id)) {
          throw new Error(`block ${block.id} is registered twice`);
        }
        ids.add(block.id);
      }
      plugins.push(plugin);
    },
    plugins: () => plugins,
    blocks: () => plugins.flatMap((p) => p.blocks ?? []),
    extensions() {
      const seen = new Set<string>();
      const out: AnyExtension[] = [];
      for (const ext of plugins.flatMap((p) => p.extensions)) {
        if (!seen.has(ext.name)) {
          seen.add(ext.name);
          out.push(ext);
        }
      }
      return out;
    },
  };
}
