import Link from "next/link";
import { notFound } from "next/navigation";

import {
  ArtifactFrame,
  DocumentView,
} from "@/features/artifacts/components/artifact-frame";
import { SaveVersionForm } from "@/features/artifacts/components/artifact-forms";
import {
  getArtifact,
  getArtifactDocument,
  listArtifactVersions,
  viewArtifact,
} from "@/lib/cowork";

/** One artifact: a version of it, its versions, and the next save. */
export default async function ArtifactView({
  params,
  searchParams,
}: {
  params: Promise<{ org: string; artifact: string }>;
  searchParams: Promise<{ version?: string }>;
}) {
  const [{ org, artifact }, { version }] = await Promise.all([
    params,
    searchParams,
  ]);
  const [a, versions] = await Promise.all([
    getArtifact(org, artifact),
    listArtifactVersions(org, artifact),
  ]);
  if (!a.ok && a.status === 404) notFound();
  if (!a.ok) {
    return (
      <p role="alert" className="text-red-700">
        {a.message}
      </p>
    );
  }
  const number = Number(version) || a.value.latest;
  const [view, document] = await Promise.all([
    a.value.kind === "html" ? viewArtifact(org, artifact, number) : null,
    a.value.kind === "document"
      ? getArtifactDocument(org, artifact, number)
      : null,
  ]);
  const shown = view ?? document;
  return (
    <article className="mx-auto flex max-w-4xl flex-col gap-4">
      <Link href={`/${org}/artifacts`} className="text-sm underline">
        ← Artifacts
      </Link>
      <h1 className="text-2xl font-semibold">{a.value.title}</h1>
      <p className="text-sm text-neutral-600">
        Version {number} of {a.value.latest} · embed it in a page with its ID,{" "}
        <code>{a.value.id}</code>
      </p>
      {shown && !shown.ok ? (
        <p role="alert" className="text-red-700">
          {shown.message}
        </p>
      ) : view?.ok ? (
        <ArtifactFrame url={view.value.url} title={a.value.title} />
      ) : document?.ok ? (
        <DocumentView document={document.value} />
      ) : null}
      {versions.ok && (
        <nav aria-label="Versions" className="flex flex-wrap gap-2 text-sm">
          {versions.value.map((v) => (
            <Link
              key={v.number}
              href={`/${org}/artifacts/${artifact}?version=${v.number}`}
              aria-current={v.number === number ? "page" : undefined}
              className="underline"
            >
              v{v.number}
            </Link>
          ))}
        </nav>
      )}
      <SaveVersionForm
        org={org}
        id={artifact}
        kind={a.value.kind}
        page={document?.ok ? document.value.page : undefined}
      />
    </article>
  );
}
