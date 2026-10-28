import { Node } from "@tiptap/core";
import { describe, expect, it } from "vitest";

import { plugins, registry } from "../manifest";
import { createRegistry, type Plugin } from "../registry";

// A block defined here, outside src/plugins/core, through the same
// interface the built-in ones use (ADR-0127 §Decision 9).
const Callout = Node.create({
  name: "callout",
  group: "block",
  content: "inline*",
  parseHTML: () => [{ tag: "aside[data-callout]" }],
  renderHTML: () => ["aside", { "data-callout": "" }, 0],
});

const calloutPlugin: Plugin = {
  name: "acme/callout",
  extensions: [Callout],
  blocks: [
    { id: "callout", node: "callout", label: "Callout", insert: () => {} },
  ],
};

describe("plugin registry", () => {
  it("takes a block from outside the core package as it takes the built-in ones", () => {
    const r = createRegistry();
    for (const p of plugins) r.register(p);
    r.register(calloutPlugin);

    expect(r.blocks().map((b) => b.id)).toContain("callout");
    expect(r.extensions().map((e) => e.name)).toContain("callout");
    expect(r.plugins().at(-1)).toBe(calloutPlugin);
  });

  it("offers every block type a page holds, all registered through the manifest", () => {
    const nodes = new Set(registry.blocks().map((b) => b.node));
    for (const node of [
      "paragraph",
      "heading",
      "bulletList",
      "orderedList",
      "taskList",
      "codeBlock",
      "blockquote",
      "horizontalRule",
      "image",
    ]) {
      expect(nodes).toContain(node);
    }
    // The registry holds nothing the manifest did not register.
    expect(registry.plugins()).toEqual(plugins);
  });

  it("names each extension once, though several plugins may need it", () => {
    const names = registry.extensions().map((e) => e.name);
    expect(new Set(names).size).toBe(names.length);
  });

  it("refuses a plugin or a block registered twice", () => {
    const r = createRegistry();
    r.register(calloutPlugin);
    expect(() => r.register(calloutPlugin)).toThrow(/registered twice/);
    expect(() =>
      r.register({ ...calloutPlugin, name: "other/callout" }),
    ).toThrow(/block callout is registered twice/);
  });
});
