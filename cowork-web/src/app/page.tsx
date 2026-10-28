import Link from "next/link";

import { env } from "@/env";
import { listOrganizations } from "@/lib/platform";

/** The workspaces the caller has: one per organization they belong to. */
export default async function HomePage() {
  const orgs = await listOrganizations();
  return (
    <main className="mx-auto flex max-w-xl flex-col gap-4 p-8">
      <h1 className="text-2xl font-semibold">cowork</h1>
      {orgs === null ? (
        <p>
          <a className="underline" href={`${env.GARAM_CONSOLE_URL}/login`}>
            Sign in at the console
          </a>{" "}
          to open your workspaces.
        </p>
      ) : orgs.length === 0 ? (
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
