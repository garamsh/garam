import "server-only";

import { cookies } from "next/headers";

import { SESSION_COOKIE } from "@garam/platform-sdk-ts/session";

import { env } from "@/env";
import type { components } from "@/types/api";

export type Page = components["schemas"]["Page"];
export type PageCreate = components["schemas"]["PageCreate"];
export type PageUpdate = components["schemas"]["PageUpdate"];

/**
 * What a call to cowork's server answered: the value, or the refusal's
 * status and the sentence it carried. A refusal is an expected outcome
 * a page renders, not a throw (stack-nextjs.md §7).
 */
export type Result<T> =
  { ok: true; value: T } | { ok: false; status: number; message: string };

async function call<T>(path: string, init?: RequestInit): Promise<Result<T>> {
  const session = (await cookies()).get(SESSION_COOKIE)?.value;
  const response = await fetch(`${env.COWORK_API_URL}${path}`, {
    ...init,
    cache: "no-store",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...(session ? { Cookie: `${SESSION_COOKIE}=${session}` } : {}),
    },
  });
  if (response.status === 204) return { ok: true, value: undefined as T };
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    return {
      ok: false,
      status: response.status,
      message: body.message ?? response.statusText,
    };
  }
  return { ok: true, value: body as T };
}

const pagesOf = (org: string) => `/orgs/${encodeURIComponent(org)}/pages`;

export async function listPages(org: string): Promise<Result<Page[]>> {
  const r = await call<{ pages: Page[] }>(pagesOf(org));
  return r.ok ? { ok: true, value: r.value.pages } : r;
}

export function getPage(org: string, id: string): Promise<Result<Page>> {
  return call<Page>(`${pagesOf(org)}/${encodeURIComponent(id)}`);
}

export function createPage(
  org: string,
  body: PageCreate,
): Promise<Result<Page>> {
  return call<Page>(pagesOf(org), {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function updatePage(
  org: string,
  id: string,
  body: PageUpdate,
): Promise<Result<Page>> {
  return call<Page>(`${pagesOf(org)}/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  });
}

export function deletePage(org: string, id: string): Promise<Result<void>> {
  return call<void>(`${pagesOf(org)}/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export type Channel = components["schemas"]["Channel"];
export type ChannelCreate = components["schemas"]["ChannelCreate"];
export type Message = components["schemas"]["Message"];
export type MessagePost = components["schemas"]["MessagePost"];

const channelsOf = (org: string) => `/orgs/${encodeURIComponent(org)}/channels`;
const channelOf = (org: string, id: string) =>
  `${channelsOf(org)}/${encodeURIComponent(id)}`;

const messagesOnly = (r: Result<{ messages: Message[] }>): Result<Message[]> =>
  r.ok ? { ok: true, value: r.value.messages } : r;

export async function listChannels(org: string): Promise<Result<Channel[]>> {
  const r = await call<{ channels: Channel[] }>(channelsOf(org));
  return r.ok ? { ok: true, value: r.value.channels } : r;
}

export function createChannel(
  org: string,
  body: ChannelCreate,
): Promise<Result<Channel>> {
  return call<Channel>(channelsOf(org), {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function getChannel(org: string, id: string): Promise<Result<Channel>> {
  return call<Channel>(channelOf(org, id));
}

export function addChannelMember(
  org: string,
  id: string,
  member: string,
): Promise<Result<void>> {
  return call<void>(`${channelOf(org, id)}/members`, {
    method: "POST",
    body: JSON.stringify({ member }),
  });
}

export function leaveChannel(org: string, id: string): Promise<Result<void>> {
  return call<void>(`${channelOf(org, id)}/members/me`, { method: "DELETE" });
}

export async function listMessages(
  org: string,
  id: string,
): Promise<Result<Message[]>> {
  return messagesOnly(
    await call<{ messages: Message[] }>(`${channelOf(org, id)}/messages`),
  );
}

export function postMessage(
  org: string,
  id: string,
  body: MessagePost,
): Promise<Result<Message>> {
  return call<Message>(`${channelOf(org, id)}/messages`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function getThread(
  org: string,
  message: string,
): Promise<Result<Message[]>> {
  return messagesOnly(
    await call<{ messages: Message[] }>(
      `/orgs/${encodeURIComponent(org)}/messages/${encodeURIComponent(message)}/thread`,
    ),
  );
}

export async function searchMessages(
  org: string,
  q: string,
): Promise<Result<Message[]>> {
  return messagesOnly(
    await call<{ messages: Message[] }>(
      `/orgs/${encodeURIComponent(org)}/messages?q=${encodeURIComponent(q)}`,
    ),
  );
}

export async function listPageMessages(
  org: string,
  page: string,
): Promise<Result<Message[]>> {
  return messagesOnly(
    await call<{ messages: Message[] }>(
      `${pagesOf(org)}/${encodeURIComponent(page)}/messages`,
    ),
  );
}

export type Task = components["schemas"]["Task"];
export type TaskStatus = components["schemas"]["TaskStatus"];
export type TaskCreate = components["schemas"]["TaskCreate"];
export type TaskUpdate = components["schemas"]["TaskUpdate"];
export type TaskComment = components["schemas"]["TaskComment"];

/** A board's columns, in order. */
export const TASK_STATUSES: readonly TaskStatus[] = ["todo", "doing", "done"];

const tasksOf = (org: string) => `/orgs/${encodeURIComponent(org)}/tasks`;
const taskOf = (org: string, id: string) =>
  `${tasksOf(org)}/${encodeURIComponent(id)}`;

/** What a listing narrows to: every field set must match. */
export type TaskFilter = Partial<
  Record<"status" | "assignee" | "page" | "message", string>
>;

export async function listTasks(
  org: string,
  filter: TaskFilter = {},
): Promise<Result<Task[]>> {
  const q = new URLSearchParams(
    Object.entries(filter).filter((e): e is [string, string] => !!e[1]),
  ).toString();
  const r = await call<{ tasks: Task[] }>(`${tasksOf(org)}${q ? `?${q}` : ""}`);
  return r.ok ? { ok: true, value: r.value.tasks } : r;
}

export function getTask(org: string, id: string): Promise<Result<Task>> {
  return call<Task>(taskOf(org, id));
}

export function createTask(
  org: string,
  body: TaskCreate,
): Promise<Result<Task>> {
  return call<Task>(tasksOf(org), {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function updateTask(
  org: string,
  id: string,
  body: TaskUpdate,
): Promise<Result<Task>> {
  return call<Task>(taskOf(org, id), {
    method: "PATCH",
    body: JSON.stringify(body),
  });
}

export async function listTaskComments(
  org: string,
  id: string,
): Promise<Result<TaskComment[]>> {
  const r = await call<{ comments: TaskComment[] }>(
    `${taskOf(org, id)}/comments`,
  );
  return r.ok ? { ok: true, value: r.value.comments } : r;
}

export function postTaskComment(
  org: string,
  id: string,
  body: string,
): Promise<Result<TaskComment>> {
  return call<TaskComment>(`${taskOf(org, id)}/comments`, {
    method: "POST",
    body: JSON.stringify({ body }),
  });
}

export type Artifact = components["schemas"]["Artifact"];
export type ArtifactVersion = components["schemas"]["ArtifactVersion"];
export type ArtifactDocument = components["schemas"]["ArtifactDocument"];
export type ArtifactCreate = components["schemas"]["ArtifactCreate"];

const artifactsOf = (org: string) =>
  `/orgs/${encodeURIComponent(org)}/artifacts`;
const artifactOf = (org: string, id: string) =>
  `${artifactsOf(org)}/${encodeURIComponent(id)}`;
const versionQuery = (version?: number) =>
  version ? `?version=${version}` : "";

export async function listArtifacts(org: string): Promise<Result<Artifact[]>> {
  const r = await call<{ artifacts: Artifact[] }>(artifactsOf(org));
  return r.ok ? { ok: true, value: r.value.artifacts } : r;
}

export function getArtifact(
  org: string,
  id: string,
): Promise<Result<Artifact>> {
  return call<Artifact>(artifactOf(org, id));
}

export function createArtifact(
  org: string,
  body: ArtifactCreate,
): Promise<Result<Artifact>> {
  return call<Artifact>(artifactsOf(org), {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function saveArtifactVersion(
  org: string,
  id: string,
  body: { html?: string; page?: string },
): Promise<Result<Artifact>> {
  return call<Artifact>(`${artifactOf(org, id)}/versions`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function listArtifactVersions(
  org: string,
  id: string,
): Promise<Result<ArtifactVersion[]>> {
  const r = await call<{ versions: ArtifactVersion[] }>(
    `${artifactOf(org, id)}/versions`,
  );
  return r.ok ? { ok: true, value: r.value.versions } : r;
}

/**
 * Where an HTML version is viewed: an address on the user-content
 * origin, good for a few minutes, which only a sandboxed iframe loads.
 */
export function viewArtifact(
  org: string,
  id: string,
  version?: number,
): Promise<Result<{ url: string; version: number }>> {
  return call(`${artifactOf(org, id)}/view${versionQuery(version)}`);
}

export function getArtifactDocument(
  org: string,
  id: string,
  version?: number,
): Promise<Result<ArtifactDocument>> {
  return call<ArtifactDocument>(
    `${artifactOf(org, id)}/document${versionQuery(version)}`,
  );
}
