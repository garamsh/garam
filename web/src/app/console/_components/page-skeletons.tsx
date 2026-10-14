import { Skeleton } from "@/components/ui/skeleton";

const LIST_ROWS = [0, 1, 2, 3, 4, 5];
const DETAIL_FACTS = [0, 1, 2, 3];

/** The shape a list page takes while its data is in flight. */
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
      <Skeleton className="h-8 w-64 max-w-full" />
      <div className="overflow-hidden rounded-xl border">
        <Skeleton className="h-9 w-full rounded-none" />
        {LIST_ROWS.map((row) => (
          <div
            key={row}
            className="flex h-row items-center gap-4 border-t px-3"
          >
            <Skeleton className="h-3.5 w-1/3" />
            <Skeleton className="h-3.5 w-1/5" />
            <Skeleton className="ml-auto h-3.5 w-16" />
          </div>
        ))}
      </div>
    </div>
  );
}

/** The shape a detail page takes while its object is being read. */
export function DetailPageSkeleton() {
  return (
    <div role="status" aria-label="Loading" className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-6 w-56" />
        <Skeleton className="h-4 w-40" />
      </div>
      <div className="flex gap-4 border-b pb-2">
        <Skeleton className="h-5 w-20" />
        <Skeleton className="h-5 w-20" />
        <Skeleton className="h-5 w-20" />
      </div>
      <div className="flex flex-col rounded-xl border px-4">
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
