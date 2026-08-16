import { Skeleton } from "@/components/ui/skeleton";

const ROWS = [0, 1, 2];

/** The shape a console page takes while its data is in flight. */
export function SectionSkeleton() {
  return (
    <div role="status" aria-label="Loading" className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Skeleton className="h-7 w-48" />
        <Skeleton className="h-4 w-72" />
      </div>
      <div className="flex flex-col gap-3">
        {ROWS.map((row) => (
          <Skeleton key={row} className="h-16 w-full rounded-xl" />
        ))}
      </div>
    </div>
  );
}
