"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import {
  createArtifact,
  getArtifact,
  getArtifactDocument,
  saveArtifactVersion,
  viewArtifact,
  type ArtifactDocument,
} from "@/lib/cowork";

export type ActionState = { error?: string };

/** Creates an artifact, an HTML page or a frozen page, and opens it. */
export async function createArtifactAction(
  org: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const kind = String(form.get("kind") ?? "html") as "html" | "document";
  const r = await createArtifact(org, {
    kind,
    title: String(form.get("title") ?? "").trim(),
    ...(kind === "html"
      ? { html: String(form.get("html") ?? "") }
      : { page: String(form.get("page") ?? "") }),
  });
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/artifacts`);
  redirect(`/${org}/artifacts/${r.value.id}`);
}

/** Saves an artifact's next version; every earlier one stays. */
export async function saveVersionAction(
  org: string,
  id: string,
  kind: "html" | "document",
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const r = await saveArtifactVersion(
    org,
    id,
    kind === "html"
      ? { html: String(form.get("html") ?? "") }
      : { page: String(form.get("page") ?? "") },
  );
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/artifacts/${id}`);
  redirect(`/${org}/artifacts/${id}`);
}

/** What an embed shows: an HTML version's address, or a document. */
export type Embed =
  | { ok: true; kind: "html"; title: string; url: string }
  | { ok: true; kind: "document"; title: string; document: ArtifactDocument }
  | { ok: false; error: string };

/**
 * Reads an artifact for an embed in a page or a message, as the
 * viewer: one the viewer may not read is answered as the refusal.
 */
export async function embedAction(org: string, id: string): Promise<Embed> {
  const a = await getArtifact(org, id);
  if (!a.ok) return { ok: false, error: a.message };
  if (a.value.kind === "html") {
    const v = await viewArtifact(org, id);
    return v.ok
      ? { ok: true, kind: "html", title: a.value.title, url: v.value.url }
      : { ok: false, error: v.message };
  }
  const d = await getArtifactDocument(org, id);
  return d.ok
    ? { ok: true, kind: "document", title: a.value.title, document: d.value }
    : { ok: false, error: d.message };
}
