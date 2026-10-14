/** The title block every console page opens with. */
export function PageHeader({
  title,
  description,
  count,
  children,
}: {
  title: string;
  description: string;
  /** How many entries a list page holds, shown beside its title. */
  count?: number;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="flex items-baseline gap-2 text-xl tracking-tight">
          {title}
          {count !== undefined && (
            <span className="text-sm font-normal text-muted-foreground tabular-nums">
              {count}
            </span>
          )}
        </h1>
        <p className="text-muted-foreground">{description}</p>
      </div>
      {children && (
        <div className="flex shrink-0 items-center gap-2">{children}</div>
      )}
    </div>
  );
}
