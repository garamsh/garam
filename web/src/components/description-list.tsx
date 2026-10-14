import { cn } from "@/lib/utils";

/** One term and what the page says about it. */
export type DescriptionItem = { term: string; description: React.ReactNode };

/**
 * Facts about one object, as term and value pairs: stacked on a narrow
 * screen, two columns from `sm` up.
 */
export function DescriptionList({
  items,
  className,
}: {
  items: readonly DescriptionItem[];
  className?: string;
}) {
  return (
    <dl
      className={cn(
        "grid grid-cols-1 gap-x-6 sm:grid-cols-[minmax(8rem,max-content)_1fr]",
        className,
      )}
    >
      {items.map((item) => (
        <div
          key={item.term}
          className="grid grid-cols-subgrid gap-1 border-b py-2.5 last:border-b-0 sm:col-span-2"
        >
          <dt className="text-muted-foreground">{item.term}</dt>
          <dd className="min-w-0 break-words">{item.description}</dd>
        </div>
      ))}
    </dl>
  );
}
