"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import {
  createTask,
  postTaskComment,
  updateTask,
  type TaskStatus,
  type TaskUpdate,
} from "@/lib/cowork";

/** What a task form shows after an action: the server's refusal, if any. */
export type ActionState = { error?: string };

const text = (form: FormData, name: string) =>
  String(form.get(name) ?? "").trim();

/**
 * Creates a task and opens it. A task linking a page or a message is
 * created from it, and links only what its writer may read: cowork's
 * server decides with the caller's own cookie.
 */
export async function createTaskAction(
  org: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const r = await createTask(org, {
    title: text(form, "title"),
    assignee: text(form, "assignee"),
    due: text(form, "due"),
    pages: form.getAll("page").map(String).filter(Boolean),
    messages: form.getAll("message").map(String).filter(Boolean),
  });
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/tasks`);
  redirect(`/${org}/tasks/${r.value.id}`);
}

/** Moves a task to status, from a list or a board. */
export async function moveTaskAction(
  org: string,
  id: string,
  status: TaskStatus,
): Promise<ActionState> {
  const r = await updateTask(org, id, { status });
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/tasks`);
  return {};
}

/** Saves a task's fields as its form holds them. */
export async function updateTaskAction(
  org: string,
  id: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const body: TaskUpdate = {
    title: text(form, "title"),
    description: String(form.get("description") ?? ""),
    status: text(form, "status") as TaskStatus,
    assignee: text(form, "assignee"),
    due: text(form, "due"),
  };
  const r = await updateTask(org, id, body);
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/tasks`, "layout");
  return {};
}

export async function commentTaskAction(
  org: string,
  id: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const r = await postTaskComment(org, id, String(form.get("body") ?? ""));
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/tasks/${id}`);
  return {};
}
