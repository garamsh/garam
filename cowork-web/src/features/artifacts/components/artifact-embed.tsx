"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { embedAction, type Embed } from "../actions";
import { ArtifactFrame, DocumentView } from "./artifact-frame";

/**
 * An artifact shown where a page or a message embeds it, read as the
 * viewer when it comes into view.
 */
export function ArtifactEmbed({ org, id }: { org: string; id: string }) {
  const [embed, setEmbed] = useState<Embed | null>(null);
  useEffect(() => {
    let live = true;
    void embedAction(org, id).then((e) => live && setEmbed(e));
    return () => {
      live = false;
    };
  }, [org, id]);
  if (!embed) return <p className="text-sm text-neutral-500">Loading…</p>;
  if (!embed.ok)
    return (
      <p role="alert" className="text-sm text-red-700">
        {embed.error}
      </p>
    );
  return (
    <figure data-testid="artifact-embed" className="flex flex-col gap-1">
      <figcaption className="text-sm">
        <Link href={`/${org}/artifacts/${id}`} className="underline">
          {embed.title}
        </Link>
      </figcaption>
      {embed.kind === "html" ? (
        <ArtifactFrame url={embed.url} title={embed.title} />
      ) : (
        <DocumentView document={embed.document} />
      )}
    </figure>
  );
}
