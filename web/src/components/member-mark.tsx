import { cn } from "@/lib/utils";

/** What a mark stands for; each kind has a shape of its own. */
export type MarkKind = "person" | "agent" | "team";

// The kind is the shape, never a colour, so the one accent stays the
// only one (ADR-0113, ADR-0103 principle 2).
const SHAPE: Record<MarkKind, string> = {
  person: "rounded-full",
  agent: "rounded-md",
  team: "rounded-md border-dashed",
};

/** Two letters for a name: a word's initials, or an agent's first two. */
function letters(kind: MarkKind, name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (kind === "agent" || words.length < 2) return name.trim().slice(0, 2);
  return `${words[0][0]}${words[1][0]}`;
}

/**
 * A member's mark: its initials in its kind's shape. It is decoration;
 * the kind is always also said in words beside it.
 */
export function MemberMark({
  kind,
  name,
  className,
}: {
  kind: MarkKind;
  name: string;
  className?: string;
}) {
  return (
    <span
      aria-hidden
      data-kind={kind}
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center border bg-muted text-[length:var(--text-caption)] font-medium text-muted-foreground",
        kind === "agent" ? "lowercase" : "uppercase",
        SHAPE[kind],
        className,
      )}
    >
      {letters(kind, name)}
    </span>
  );
}
