import Link from "next/link";
import { notFound } from "next/navigation";

import { NewPageForm } from "@/features/pages/components/page-forms";
import { PageTree } from "@/features/pages/components/page-tree";
import { listPages } from "@/lib/cowork";

/** A workspace: its page tree beside whatever page is open. */
export default async function WorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ org: string }>;
}) {
  const { org } = await params;
  const pages = await listPages(org);
  if (!pages.ok && pages.status === 404) notFound();
  return (
    <div className="grid min-h-screen grid-cols-[16rem_1fr]">
      <aside className="flex flex-col gap-4 border-r border-neutral-200 p-4">
        <Link href={`/${org}`} className="font-semibold">
          {org}
        </Link>
        <nav aria-label="Workspace" className="flex flex-col gap-1 text-sm">
          <Link href={`/${org}/channels`} className="underline">
            Channels
          </Link>
          <Link href={`/${org}/search`} className="underline">
            Search
          </Link>
        </nav>
        {pages.ok ? (
          <PageTree org={org} pages={pages.value} />
        ) : (
          <p role="alert" className="text-sm text-red-700">
            {pages.message}
          </p>
        )}
        <NewPageForm org={org} />
      </aside>
      <main className="p-8">{children}</main>
    </div>
  );
}
