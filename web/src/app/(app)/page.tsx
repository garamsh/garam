import { redirect } from "next/navigation";

/**
 * The backend redirects to `/` after a successful OAuth callback, so
 * the root forwards to the dashboard rather than duplicating it.
 */
export default function HomePage() {
  redirect("/orgs");
}
