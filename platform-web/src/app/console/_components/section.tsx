import { useId } from "react";

import { cn } from "@garam/design/lib/utils";

/**
 * One titled part of a page: its heading, with the count where the part
 * is a list, what it holds in a sentence, and the part itself. `region`
 * names it by its heading, making it a landmark, for a page's major
 * parts rather than the cards inside a tab.
 */
export function Section({
  title,
  description,
  count,
  region = false,
  className,
  children,
}: {
  title: string;
  description?: string;
  /** Omitted when the list could not be read, so no count is guessed. */
  count?: number;
  region?: boolean;
  className?: string;
  children: React.ReactNode;
}) {
  const id = useId();
  return (
    <section
      aria-labelledby={region ? id : undefined}
      className={cn("flex flex-col gap-3", className)}
    >
      <div className="flex flex-col gap-1">
        <h2 id={id} className="flex items-baseline gap-2 text-base">
          {title}
          {count !== undefined && (
            <span className="text-sm font-normal text-muted-foreground tabular-nums">
              {count}
            </span>
          )}
        </h2>
        {description !== undefined && (
          <p className="text-muted-foreground">{description}</p>
        )}
      </div>
      {children}
    </section>
  );
}
