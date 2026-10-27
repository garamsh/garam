import Link from "next/link";

import { NewArtifactForm } from "@/features/artifacts/components/artifact-forms";
import { listArtifacts, listPages } from "@/lib/cowork";

/** The workspace's artifacts, and the form that adds one. */
export default async function ArtifactsPage({
  params,
}: {
  params: Promise<{ org: string }>;
}) {
  const { org } = await params;
  const [artifacts, pages] = await Promise.all([
    listArtifacts(org),
    listPages(org),
  ]);
  return (
    <section className="mx-auto flex max-w-3xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">Artifacts</h1>
      {artifacts.ok ? (
        <ul aria-label="Artifacts" className="flex flex-col gap-1">
          {artifacts.value.map((a) => (
            <li key={a.id} className="flex items-baseline gap-2">
              <Link href={`/${org}/artifacts/${a.id}`} className="underline">
                {a.title}
              </Link>
              <span className="text-xs text-neutral-600">
                {a.kind === "html" ? "HTML" : "Document"} · v{a.latest}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <p role="alert" className="text-red-700">
          {artifacts.message}
        </p>
      )}
      <NewArtifactForm
        org={org}
        pages={
          pages.ok ? pages.value.map((p) => ({ id: p.id, title: p.title })) : []
        }
      />
    </section>
  );
}
