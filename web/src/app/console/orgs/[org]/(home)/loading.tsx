import { DetailPageSkeleton } from "../../../_components/page-skeletons";

/**
 * Home while its reads are in flight. It is the organization's own
 * page, so it takes the detail page's shape — a header over facts —
 * rather than the list shape every section below shares.
 */
export default function Loading() {
  return <DetailPageSkeleton />;
}
