import { ListPageSkeleton } from "../../_components/page-skeletons";

/**
 * Stands in for every section below the organization: each one opens
 * on a heading over a list. An entry's page sets its own.
 */
export default function Loading() {
  return <ListPageSkeleton />;
}
