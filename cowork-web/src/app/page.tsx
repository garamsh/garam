import Link from "next/link";

import { listOrganizations } from "@/lib/platform";
import { redirectToSignIn } from "@/lib/sign-in-redirect";

/** The workspaces the caller has: one per organization they belong to. */
export default async function HomePage() {
  const orgs = await listOrganizations();
  if (orgs === null) return redirectToSignIn();
  return (
    <main className="mx-auto flex max-w-xl flex-col gap-4 p-8">
      <h1 className="text-2xl font-semibold">cowork</h1>
      {orgs.length === 0 ? (
        <p>You belong to no organization yet.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {orgs.map((o) => (
            <li key={o.slug}>
              <Link className="underline" href={`/${o.slug}`}>
                {o.name}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
