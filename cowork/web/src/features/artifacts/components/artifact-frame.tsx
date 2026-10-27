import type { ArtifactDocument } from "@/lib/cowork";

/**
 * An HTML version, loaded from the user-content origin into an iframe
 * sandboxed without that origin: its scripts run, and can reach no
 * cookie, no storage and no cowork API (ADR-0127 §8).
 */
export function ArtifactFrame({ url, title }: { url: string; title: string }) {
  return (
    <iframe
      src={url}
      title={title}
      sandbox="allow-scripts"
      referrerPolicy="no-referrer"
      className="h-96 w-full rounded border border-neutral-200 bg-white"
    />
  );
}

type Block = {
  type?: string;
  text?: string;
  attrs?: { level?: number };
  items?: { text: string; checked?: boolean }[];
};

/** A document version, its frozen blocks read and never edited. */
export function DocumentView({ document }: { document: ArtifactDocument }) {
  return (
    <div
      data-testid="artifact-document"
      className="flex flex-col gap-2 rounded border border-neutral-200 p-3"
    >
      {(document.blocks as Block[]).map((b, i) => {
        if (b.type === "heading")
          return (
            <p key={i} className="font-semibold">
              {b.text}
            </p>
          );
        if (b.items)
          return (
            <ul key={i} className="list-disc pl-5">
              {b.items.map((it, j) => (
                <li key={j}>
                  {b.type === "taskList" && (it.checked ? "☑ " : "☐ ")}
                  {it.text}
                </li>
              ))}
            </ul>
          );
        if (b.type === "horizontalRule") return <hr key={i} />;
        return (
          <p key={i} className="whitespace-pre-wrap">
            {b.text}
          </p>
        );
      })}
    </div>
  );
}
