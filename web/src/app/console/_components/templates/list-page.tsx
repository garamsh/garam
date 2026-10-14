import { PageHeader } from "../page-header";

/**
 * A page listing one kind of object: its header with the count and the
 * primary action, then the filters, then the list itself — a
 * `DataTable`, which brings its own empty state.
 */
export function ListPage({
  title,
  description,
  count,
  primaryAction,
  filters,
  children,
}: {
  title: string;
  description: string;
  /** Omitted when the list could not be read, so no count is guessed. */
  count?: number;
  primaryAction?: React.ReactNode;
  filters?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={title} description={description} count={count}>
        {primaryAction}
      </PageHeader>
      {filters && (
        <div className="flex flex-wrap items-center gap-2">{filters}</div>
      )}
      {children}
    </div>
  );
}
