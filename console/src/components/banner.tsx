import {
  CircleAlert,
  CircleCheck,
  Info,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";

/** What a banner reports. */
export type BannerTone = "info" | "ok" | "warn" | "danger";

const TONE: Record<
  BannerTone,
  { icon: LucideIcon; frame: string; glyph: string }
> = {
  info: { icon: Info, frame: "border-info/30 bg-info/8", glyph: "text-info" },
  ok: { icon: CircleCheck, frame: "border-ok/30 bg-ok/8", glyph: "text-ok" },
  warn: {
    icon: TriangleAlert,
    frame: "border-warn/40 bg-warn/10",
    glyph: "text-warn",
  },
  danger: {
    icon: CircleAlert,
    frame: "border-danger/30 bg-danger/8",
    glyph: "text-danger",
  },
};

/**
 * A notice about the page or the section it stands in: a failing state,
 * a read that did not answer, a consequence to weigh. A danger banner is
 * announced as an alert; the others are polite status.
 */
export function Banner({
  tone,
  title,
  children,
  action,
  className,
}: {
  tone: BannerTone;
  title: string;
  children?: React.ReactNode;
  /** A control that resolves what the banner reports. */
  action?: React.ReactNode;
  className?: string;
}) {
  const { icon: Icon, frame, glyph } = TONE[tone];
  return (
    <div
      role={tone === "danger" ? "alert" : "status"}
      data-tone={tone}
      className={cn(
        "flex flex-wrap items-start gap-3 rounded-xl border px-4 py-3",
        frame,
        className,
      )}
    >
      <Icon aria-hidden className={cn("mt-0.5 size-4 shrink-0", glyph)} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="font-medium">{title}</p>
        {children && <div className="text-muted-foreground">{children}</div>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}
