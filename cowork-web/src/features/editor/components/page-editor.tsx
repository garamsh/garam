"use client";

import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { EditorContent, useEditor } from "@tiptap/react";
import { useCallback, useEffect, useState } from "react";
import { WebsocketProvider } from "y-websocket";
import * as Y from "yjs";

import { ArtifactEmbed } from "@/features/artifacts/components/artifact-embed";
import { BlockToolbar } from "@/features/editor/components/block-toolbar";
import { leaveOnRefusal } from "@/lib/socket-close";
import { EmbedContext, type Embed } from "@/plugins/embed";
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
 *
 * The provider is made once mounted, and the editor only once the
 * provider exists, so exactly one editor is ever bound to the
 * document: two would each apply the same local edits to it.
 */
export function PageEditor({ org, page, syncUrl, user }: Props) {
  const [session, setSession] = useState<{
    doc: Y.Doc;
    provider: WebsocketProvider;
  } | null>(null);
  const [status, setStatus] = useState<Status>("connecting");
  const [readOnly, setReadOnly] = useState(false);

  useEffect(() => {
    const doc = new Y.Doc();
    // The provider reaches `<url>/<room>`: the room is the page's
    // place under the org, and its sync path.
    const provider = new WebsocketProvider(
      `${syncUrl}/orgs/${encodeURIComponent(org)}/pages`,
      `${encodeURIComponent(page)}/sync`,
      doc,
    );
    provider.on("status", ({ status }: { status: Status }) => {
      setStatus(status);
      // A connection's write access is its own, decided at each
      // upgrade, so a refusal does not outlive the one it came on.
      if (status === "connected") setReadOnly(false);
    });
    provider.on("closed", ({ code }: { code: number }) => leaveOnRefusal(code));
    // The server answers an edit it refuses with y-websocket's
    // permission-denied message, which the provider hands here.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (provider as any).permissionDeniedHandler = () => setReadOnly(true);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the provider exists only once mounted
    setSession({ doc, provider });
    return () => {
      provider.destroy();
      doc.destroy();
    };
  }, [org, page, syncUrl]);

  // What a plugin's embed block renders: the workspace's artifact, read
  // as the viewer.
  const embed: Embed = useCallback(
    ({ id }: { kind: "artifact"; id: string }) => (
      <ArtifactEmbed org={org} id={id} />
    ),
    [org],
  );

  return session ? (
    <EmbedContext.Provider value={embed}>
      <CollaborativeEditor
        doc={session.doc}
        provider={session.provider}
        user={user}
        status={status}
        readOnly={readOnly}
      />
    </EmbedContext.Provider>
  ) : (
    <p className="text-sm text-neutral-500" data-testid="sync-status">
      connecting
    </p>
  );
}

/**
 * Another person's caret: a coloured bar whose name is drawn by CSS
 * from `data-name`, so the label adds no text to the page's content,
 * as read aloud or copied.
 */
function renderCaret(user: { name?: string; color?: string }): HTMLElement {
  const caret = document.createElement("span");
  caret.classList.add("collaboration-carets__caret");
  caret.setAttribute("aria-hidden", "true");
  caret.dataset.name = user.name ?? "";
  caret.style.setProperty("--caret-color", user.color ?? "#555555");
  return caret;
}

type EditorProps = {
  doc: Y.Doc;
  provider: WebsocketProvider;
  user: Props["user"];
  status: Status;
  readOnly: boolean;
};

function CollaborativeEditor({
  doc,
  provider,
  user,
  status,
  readOnly,
}: EditorProps) {
  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      ...registry.extensions(),
      Collaboration.configure({ document: doc }),
      CollaborationCaret.configure({ provider, user, render: renderCaret }),
    ],
    editorProps: {
      attributes: {
        role: "textbox",
        "aria-multiline": "true",
        "aria-label": "Page content",
        class: "page-content",
      },
    },
  });

  useEffect(() => {
    editor?.setEditable(!readOnly);
  }, [editor, readOnly]);

  return (
    <section className="flex flex-col gap-3">
      <BlockToolbar
        blocks={registry.blocks()}
        editor={editor}
        disabled={readOnly}
        status={readOnly ? "read only" : status}
      />
      <EditorContent
        editor={editor}
        className="min-h-64 rounded border border-neutral-200 p-4"
      />
    </section>
  );
}
