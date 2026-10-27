"use client";

import { useEffect, useState } from "react";

import type { Message } from "@/lib/cowork";

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
 * A channel's messages, or one thread's, as posted: the server's read
 * first, then every message that arrives on the channel's live feed,
 * with no reload.
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
    ws.onopen = () => setLive(true);
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
    return () => ws.close();
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
