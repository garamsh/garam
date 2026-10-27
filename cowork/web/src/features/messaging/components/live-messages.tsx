"use client";

import { useEffect, useState } from "react";

import type { Message } from "@/lib/cowork";

import { readMessagesAction } from "../actions";

import { MessageItem, type Names } from "./message-item";

type Props = {
  org: string;
  channel: string;
  /** Show the thread this message starts, rather than the channel. */
  thread?: string;
  initial: Message[];
  syncUrl: string;
  names: Names;
  pages: Record<string, string>;
};

/**
 * Joins what a read answered to what the feed already delivered: the
 * read's messages in its order, then any it did not yet hold.
 */
function merge(read: Message[], current: Message[]): Message[] {
  const ids = new Set(read.map((m) => m.id));
  return [...read, ...current.filter((m) => !ids.has(m.id))];
}

/**
 * A channel's messages, or one thread's, as posted: the server's read
 * first, then every message that arrives on the channel's live feed,
 * with no reload. Once the feed is open the list is read again, so a
 * message posted between the page's read and the feed opening is not
 * missed.
 */
export function LiveMessages({
  org,
  channel,
  thread,
  initial,
  syncUrl,
  names,
  pages,
}: Props) {
  const [messages, setMessages] = useState(initial);
  const [live, setLive] = useState(false);

  useEffect(() => {
    const ws = new WebSocket(
      `${syncUrl}/orgs/${encodeURIComponent(org)}/channels/${encodeURIComponent(channel)}/live`,
    );
    let open = true;
    ws.onopen = () => {
      setLive(true);
      void readMessagesAction(org, channel, thread).then(
        (read) => open && setMessages((current) => merge(read, current)),
      );
    };
    ws.onclose = () => setLive(false);
    ws.onmessage = (event) => {
      const m = JSON.parse(String(event.data)) as Message;
      setMessages((current) => {
        if (thread) {
          return m.thread === thread && !current.some((c) => c.id === m.id)
            ? [...current, m]
            : current;
        }
        if (m.thread) {
          // A reply shows in the channel as its thread's count.
          return current.map((c) =>
            c.id === m.thread ? { ...c, replies: c.replies + 1 } : c,
          );
        }
        return current.some((c) => c.id === m.id) ? current : [...current, m];
      });
    };
    return () => {
      open = false;
      ws.close();
    };
  }, [org, channel, thread, syncUrl]);

  return (
    <section className="flex flex-col gap-2">
      <span className="text-xs text-neutral-500" data-testid="live-status">
        {live ? "live" : "connecting"}
      </span>
      <ol
        className="flex flex-col gap-4"
        aria-label={thread ? "Thread" : "Messages"}
      >
        {messages.map((m) => (
          <MessageItem
            key={m.id}
            org={org}
            message={m}
            names={names}
            pages={pages}
            threadHref={
              thread ? undefined : `/${org}/channels/${channel}?thread=${m.id}`
            }
          />
        ))}
      </ol>
    </section>
  );
}
