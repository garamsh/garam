import { cn } from "@/lib/utils";

/** What a status means; the colour follows from it and never alone. */
export type StatusTone = "ok" | "warn" | "danger" | "info" | "neutral";

const DOT: Record<StatusTone, string> = {
  ok: "bg-ok",
  warn: "bg-warn",
  danger: "bg-danger",
  info: "bg-info",
  neutral: "bg-muted-foreground/60",
};

/**
 * A status as a dot and a label. The label carries the meaning, so the
 * status reads the same without colour; the dot is decoration.
 */
export function StatusDot({
  tone,
  label,
  className,
}: {
  tone: StatusTone;
  label: string;
  className?: string;
}) {
  return (
    <span
      data-tone={tone}
      className={cn(
        "inline-flex items-center gap-1.5 text-sm whitespace-nowrap",
        className,
      )}
    >
      <span
        aria-hidden
        className={cn("size-2 shrink-0 rounded-full", DOT[tone])}
      />
      {label}
    </span>
  );
}
