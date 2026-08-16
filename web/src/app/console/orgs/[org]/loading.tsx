import { SectionSkeleton } from "../../_components/section-skeleton";

/**
 * Stands in for every section below the organization: each one renders
 * a heading over a list, so they share one skeleton.
 */
export default function Loading() {
  return <SectionSkeleton />;
}
