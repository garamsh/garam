import { NotFoundBlock } from "../../_components/not-found-block";

/**
 * What `notFound()` renders inside an organization. Answering it here
 * rather than at the console keeps the organization's layout, and the
 * role it reads, mounted around the page.
 */
export default function OrgNotFound() {
  return <NotFoundBlock />;
}
