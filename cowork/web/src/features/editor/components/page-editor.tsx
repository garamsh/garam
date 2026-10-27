"use client";

import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { EditorContent, useEditor } from "@tiptap/react";
import { useEffect, useMemo, useState } from "react";
import { WebsocketProvider } from "y-websocket";
import * as Y from "yjs";

import { registry } from "@/plugins/manifest";

type Props = {
  org: string;
  page: string;
  /** cowork's server as the browser reaches it: `ws://` or `wss://`. */
  syncUrl: string;
  /** Who the caret shows to the others on the page. */
  user: { name: string; color: string };
};

type Status = "connecting" | "connected" | "disconnected";

/**
 * A page's content, edited live with everyone else on it. The document
 * is Yjs, synced with cowork's server over y-websocket's protocol
 * (ADR-0127); every block type comes from the plugin registry.
 */
export function PageEditor({ org, page, syncUrl, user }: Props) {
  const doc = useMemo(() => new Y.Doc(), []);
  const [provider, setProvider] = useState<WebsocketProvider | null>(null);
  const [status, setStatus] = useState<Status>("connecting");
  const [readOnly, setReadOnly] = useState(false);

  useEffect(() => {
    // The provider reaches `<url>/<room>`: the room is the page's
    // place under the org, and its sync path.
    const p = new WebsocketProvider(
      `${syncUrl}/orgs/${encodeURIComponent(org)}/pages`,
      `${encodeURIComponent(page)}/sync`,
      doc,
    );
    p.on("status", ({ status }: { status: Status }) => setStatus(status));
    // The server answers an edit it refuses with y-websocket's
    // permission-denied message, which the provider hands here.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (p as any).permissionDeniedHandler = () => setReadOnly(true);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the provider exists only once mounted
    setProvider(p);
    return () => {
      p.destroy();
    };
  }, [doc, org, page, syncUrl]);

  const editor = useEditor(
    {
      immediatelyRender: false,
      extensions: [
        ...registry.extensions(),
        Collaboration.configure({ document: doc }),
        ...(provider ? [CollaborationCaret.configure({ provider, user })] : []),
      ],
      editorProps: {
        attributes: {
          role: "textbox",
          "aria-multiline": "true",
          "aria-label": "Page content",
          class: "page-content",
        },
      },
    },
    [provider],
  );

  useEffect(() => {
    editor?.setEditable(!readOnly);
  }, [editor, readOnly]);

  return (
    <section className="flex flex-col gap-3">
      <div
        className="flex flex-wrap items-center gap-2"
        role="toolbar"
        aria-label="Insert a block"
      >
        {registry.blocks().map((block) => (
          <button
            key={block.id}
            type="button"
            className="rounded border border-neutral-300 px-2 py-1 text-sm hover:bg-neutral-100 disabled:opacity-50"
            disabled={!editor || readOnly}
            onClick={() => editor && block.insert(editor)}
          >
            {block.label}
          </button>
        ))}
        <span
          className="ml-auto text-xs text-neutral-500"
          data-testid="sync-status"
        >
          {readOnly ? "read only" : status}
        </span>
      </div>
      <EditorContent
        editor={editor}
        className="min-h-64 rounded border border-neutral-200 p-4"
      />
    </section>
  );
}
