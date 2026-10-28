import { diffLines, diffStat } from "@/lib/line-diff";
import { cn } from "@garam/design/lib/utils";

const MARK = { same: " ", added: "+", removed: "−" } as const;
const SPOKEN = { same: "", added: "Added: ", removed: "Removed: " } as const;

/**
 * Two versions of a text, line by line: what the change removes and
 * what it adds, each said in words as well as by its mark and tint, so
 * a diff reads the same without colour. The tint is the line's
 * background; its text stays a neutral that holds 4.5:1 on it. `before` and `after` name the
 * two sides in the caption ("v2", "this draft").
 */
export function JsonDiff({
  before,
  after,
  beforeLabel,
  afterLabel,
  className,
}: {
  before: string;
  after: string;
  beforeLabel: string;
  afterLabel: string;
  className?: string;
}) {
  const lines = diffLines(before, after);
  const { added, removed } = diffStat(lines);
  const unchanged = added === 0 && removed === 0;

  return (
    <figure
      data-slot="json-diff"
      className={cn("flex min-w-0 flex-col gap-2", className)}
    >
      <figcaption className="flex flex-wrap items-baseline justify-between gap-x-3 text-muted-foreground">
        <span>
          {beforeLabel} → {afterLabel}
        </span>
        <span className="font-mono text-table tabular-nums">
          {unchanged ? (
            "No changes"
          ) : (
            <>
              <span>+{added}</span> <span>−{removed}</span>
              <span className="sr-only">
                {` (${added} lines added, ${removed} removed)`}
              </span>
            </>
          )}
        </span>
      </figcaption>
      {/* A scroll region a keyboard can reach: long lines and long
          diffs scroll, and arrow keys scroll only what holds focus. */}
      <div
        role="region"
        aria-label={`Lines from ${beforeLabel} to ${afterLabel}`}
        tabIndex={0}
        className="max-h-80 overflow-auto rounded-lg border bg-muted/30 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <ol className="m-0 min-w-max list-none p-0 py-1 font-mono text-table leading-[1.6]">
          {lines.map((line, index) => (
            <li
              key={index}
              data-kind={line.kind}
              className={cn(
                "grid grid-cols-[2.5rem_1.25rem_1fr] pr-3",
                line.kind === "added" && "bg-ok/10",
                line.kind === "removed" && "bg-danger/10",
              )}
            >
              <span
                aria-hidden
                className="pr-2 text-right text-muted-foreground tabular-nums select-none"
              >
                {line.kind === "removed" ? line.before : line.after}
              </span>
              <span
                aria-hidden
                className="text-center text-muted-foreground select-none"
              >
                {MARK[line.kind]}
              </span>
              <span className="whitespace-pre">
                {SPOKEN[line.kind] && (
                  <span className="sr-only">{SPOKEN[line.kind]}</span>
                )}
                {line.text}
              </span>
            </li>
          ))}
        </ol>
      </div>
    </figure>
  );
}
