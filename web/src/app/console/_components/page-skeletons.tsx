import { Skeleton } from "@/components/ui/skeleton";

const LIST_ROWS = [0, 1, 2, 3, 4];
const DETAIL_FACTS = [0, 1, 2, 3];

/**
 * The shape a list page takes while its data is in flight: the header
 * and a table's rows, as `ListPage` and the table primitive draw them —
 * no card and no filter bar, which a list has only once it asks for one.
 */
export function ListPageSkeleton() {
  return (
    <div role="status" aria-label="Loading" className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-4 w-72 max-w-full" />
        </div>
        <Skeleton className="h-8 w-28" />
      </div>
      <div className="flex flex-col">
        <div className="flex h-row items-center gap-4 border-b px-2">
          <Skeleton className="h-3 w-20" />
          <Skeleton className="ml-[30%] h-3 w-16" />
        </div>
        {LIST_ROWS.map((row) => (
          <div
            key={row}
            className="flex h-row items-center gap-4 border-b px-2 last:border-b-0"
          >
            <Skeleton className="h-3.5 w-1/4" />
            <Skeleton className="ml-[5%] h-3.5 w-1/6" />
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * The shape a detail page takes while its object is being read: the
 * object header, the tab row and the facts, as `DetailPage` draws them.
 */
export function DetailPageSkeleton() {
  return (
    <div role="status" aria-label="Loading" className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-6 w-56" />
        <Skeleton className="h-5 w-32" />
      </div>
      <div className="flex gap-4 border-b pb-2">
        <Skeleton className="h-5 w-16" />
        <Skeleton className="h-5 w-16" />
        <Skeleton className="h-5 w-16" />
      </div>
      <div className="flex flex-col">
        {DETAIL_FACTS.map((fact) => (
          <div key={fact} className="flex gap-6 border-b py-3 last:border-b-0">
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-4 w-48" />
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * The shape a settings page takes while it is prepared: the header,
 * then one section's card, as `SettingsPage` draws them.
 */
export function SettingsPageSkeleton() {
  return (
    <div role="status" aria-label="Loading" className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-4 w-72 max-w-full" />
      </div>
      <div className="grid gap-8 md:grid-cols-[11rem_1fr]">
        <Skeleton className="hidden h-5 w-24 md:block" />
        <div className="flex flex-col gap-4 rounded-xl border p-4">
          <Skeleton className="h-5 w-32" />
          <Skeleton className="h-8 w-full max-w-sm" />
          <Skeleton className="h-48 w-full" />
        </div>
      </div>
    </div>
  );
}
