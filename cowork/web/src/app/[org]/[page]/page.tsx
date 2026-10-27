import Link from "next/link";
import { notFound } from "next/navigation";

import { env } from "@/env";
import { PageEditor } from "@/features/editor/components/page-editor";
import {
  DeletePageButton,
  NewPageForm,
  RenamePageForm,
} from "@/features/pages/components/page-forms";
import { getPage, listPageMessages } from "@/lib/cowork";
import { me } from "@/lib/platform";

/** A colour from a GRN, so each person's caret reads the same everywhere. */
function colorOf(grn: string): string {
  let h = 0;
  for (const c of grn) h = (h * 31 + c.charCodeAt(0)) % 360;
  return `hsl(${h} 70% 40%)`;
}

export default async function PageView({
  params,
}: {
  params: Promise<{ org: string; page: string }>;
}) {
  const { org, page } = await params;
  const [p, viewer, linking] = await Promise.all([
    getPage(org, page),
    me(),
    listPageMessages(org, page),
  ]);
  if (!p.ok && p.status === 404) notFound();
  if (!p.ok) {
    return (
      <p role="alert" className="text-red-700">
        {p.message}
      </p>
    );
  }
  return (
    <article className="mx-auto flex max-w-3xl flex-col gap-4">
      <RenamePageForm
        key={p.value.id}
        org={org}
        id={p.value.id}
        title={p.value.title}
      />
      <PageEditor
        org={org}
        page={p.value.id}
        syncUrl={env.COWORK_SYNC_URL}
        user={{
          name: viewer?.nickname ?? "",
          color: colorOf(viewer?.grn ?? ""),
        }}
      />
      {linking.ok && linking.value.length > 0 && (
        <section
          aria-label="Linked from messages"
          className="flex flex-col gap-1 text-sm"
        >
          <h2 className="font-semibold">Linked from messages</h2>
          <ul className="flex flex-col gap-1">
            {linking.value.map((m) => (
              <li key={m.id}>
                <Link
                  className="underline"
                  href={`/${org}/channels/${m.channel}?thread=${m.thread || m.id}`}
                >
                  {m.body.length > 80 ? `${m.body.slice(0, 80)}…` : m.body}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
      <div className="flex items-center justify-between gap-4">
        <NewPageForm org={org} parent={p.value.id} />
        <DeletePageButton org={org} id={p.value.id} />
      </div>
    </article>
  );
}
