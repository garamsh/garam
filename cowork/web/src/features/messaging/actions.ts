"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import {
  addChannelMember,
  createChannel,
  getThread,
  leaveChannel,
  listMessages,
  postMessage,
  type ChannelCreate,
  type Message,
} from "@/lib/cowork";

/** What a messaging form shows after an action: the server's refusal, if any. */
export type ActionState = { error?: string; sent?: number };

/**
 * Creates a channel, or starts a direct message, and opens it.
 * cowork's server decides with the caller's own cookie (ADR-0128).
 */
export async function createChannelAction(
  org: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const kind = String(form.get("kind") ?? "public") as ChannelCreate["kind"];
  const members = form.getAll("member").map(String);
  const body: ChannelCreate =
    kind === "direct"
      ? { kind, members }
      : { kind, name: String(form.get("name") ?? "").trim(), members };
  const r = await createChannel(org, body);
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}`, "layout");
  redirect(`/${org}/channels/${r.value.id}`);
}

/**
 * Posts a message, or a reply where thread is set. The message reaches
 * every reader live, the author included, so nothing is revalidated.
 */
export async function postMessageAction(
  org: string,
  channel: string,
  thread: string,
  prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const r = await postMessage(org, channel, {
    body: String(form.get("body") ?? ""),
    ...(thread ? { thread } : {}),
    mentions: form.getAll("mention").map(String),
    pages: form.getAll("page").map(String).filter(Boolean),
    artifacts: form.getAll("artifact").map(String).filter(Boolean),
  });
  if (!r.ok) return { error: r.message, sent: prev.sent };
  return { sent: (prev.sent ?? 0) + 1 };
}

export async function addMemberAction(
  org: string,
  channel: string,
  _prev: ActionState,
  form: FormData,
): Promise<ActionState> {
  const r = await addChannelMember(
    org,
    channel,
    String(form.get("member") ?? ""),
  );
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}/channels/${channel}`);
  return {};
}

export async function leaveChannelAction(
  org: string,
  channel: string,
): Promise<ActionState> {
  const r = await leaveChannel(org, channel);
  if (!r.ok) return { error: r.message };
  revalidatePath(`/${org}`, "layout");
  redirect(`/${org}/channels`);
}

/**
 * Reads a channel's messages, or a thread's, again: what a live list
 * missed between the page's read and its feed opening. A refusal reads
 * as nothing new.
 */
export async function readMessagesAction(
  org: string,
  channel: string,
  thread?: string,
): Promise<Message[]> {
  const r = thread
    ? await getThread(org, thread)
    : await listMessages(org, channel);
  return r.ok ? r.value : [];
}
