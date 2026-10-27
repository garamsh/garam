"use client";

import { mergeAttributes, Node } from "@tiptap/core";
import {
  NodeViewWrapper,
  ReactNodeViewRenderer,
  type NodeViewProps,
} from "@tiptap/react";
import { useEmbed } from "../embed";
import type { Plugin } from "../registry";

function ArtifactView({ node }: NodeViewProps) {
  const embed = useEmbed();
  const id = String(node.attrs.artifact ?? "");
  return (
    <NodeViewWrapper
      contentEditable={false}
      className="my-2"
      data-artifact={id}
    >
      {embed?.({ kind: "artifact", id })}
    </NodeViewWrapper>
  );
}

/**
 * An artifact embedded in a page, by its ID: the block keeps the ID
 * alone, and each reader sees the artifact as they may read it.
 */
const ArtifactNode = Node.create({
  name: "artifact",
  group: "block",
  atom: true,
  addAttributes() {
    return { artifact: { default: "" } };
  },
  parseHTML() {
    return [{ tag: "div[data-artifact]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-artifact": HTMLAttributes.artifact,
      }),
    ];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ArtifactView);
  },
});

export const artifactPlugin: Plugin = {
  name: "core/artifact",
  extensions: [ArtifactNode],
  blocks: [
    {
      id: "artifact",
      node: "artifact",
      label: "Artifact",
      insert: (editor) => {
        const artifact = window.prompt("Artifact ID")?.trim();
        if (artifact)
          editor
            .chain()
            .focus()
            .insertContent({ type: "artifact", attrs: { artifact } })
            .run();
      },
    },
  ],
};
