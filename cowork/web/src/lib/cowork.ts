import "server-only";

import { cookies } from "next/headers";

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

/** The platform's session cookie, which cowork's server is handed. */
export const SESSION_COOKIE = "garam_session";

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
