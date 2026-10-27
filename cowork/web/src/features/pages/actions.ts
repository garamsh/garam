"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { createPage, deletePage, updatePage } from "@/lib/cowork";

/** What a page form shows after a refused action: the server's sentence. */
export type ActionState = { error?: string };

/**
 * Creates a page and opens it. cowork's server asks the platform
 * whether the caller may, with the caller's own cookie, so nothing
 * here decides that (stack-nextjs.md §5: every call is checked again).
 */
export async function createPageAction(
  org: string,
  parent: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const title = String(form.get("title") ?? "").trim() || "Untitled";
  const r = await createPage(org, parent ? { title, parent } : { title });
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}`, "layout");
  redirect(`/${org}/${r.value.id}`);
}

export async function renamePageAction(
  org: string,
  id: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const r = await updatePage(org, id, {
    title: String(form.get("title") ?? ""),
  });
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}`, "layout");
  return {};
}

export async function deletePageAction(
  org: string,
  id: string,
): Promise<ActionState> {
  const r = await deletePage(org, id);
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}`, "layout");
  redirect(`/${org}`);
}
