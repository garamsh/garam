import { ListPageSkeleton } from "../../_components/page-skeletons";

/**
 * Stands in for every section below the organization: each one opens
 * on a heading over a list. The overview and an entry's page set
 * their own.
 */
export default function Loading() {
  return <ListPageSkeleton />;
}
