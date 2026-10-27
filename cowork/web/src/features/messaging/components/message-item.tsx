import Link from "next/link";

import type { Message } from "@/lib/cowork";

export type Names = Record<string, string>;

type Props = {
  org: string;
  message: Message;
  /** Display names by GRN, for authors and mentions. */
  names: Names;
  /** Page titles by ID, for the pages a message links. */
  pages: Record<string, string>;
  /** Where "replies" links, unset inside a thread. */
  threadHref?: string;
};

const nameOf = (names: Names, grn: string) =>
  names[grn] ?? grn.split(":").at(-1) ?? grn;

/** One message: who wrote it, what it says, whom and which pages it names. */
export function MessageItem({ org, message, names, pages, threadHref }: Props) {
  const undelivered = new Set(message.undeliveredMentions);
  return (
    <li
      className="flex flex-col gap-1"
      data-testid="message"
      data-message-id={message.id}
    >
      <div className="flex items-baseline gap-2 text-sm">
        <span className="font-semibold">{nameOf(names, message.author)}</span>
        <time className="text-xs text-neutral-500" dateTime={message.createdAt}>
          {new Date(message.createdAt).toLocaleTimeString()}
        </time>
      </div>
      <p className="whitespace-pre-wrap">{message.body}</p>
      {(message.mentions.length > 0 || message.pages.length > 0) && (
        <div className="flex flex-wrap gap-1 text-xs">
          {message.mentions.map((g) => (
            <span
              key={g}
              className="rounded bg-sky-50 px-1 text-sky-800"
              title={
                undelivered.has(g)
                  ? "Not delivered to this agent yet"
                  : undefined
              }
            >
              @{nameOf(names, g)}
              {undelivered.has(g) && " (not delivered)"}
            </span>
          ))}
          {message.pages.map((p) => (
            <Link
              key={p}
              href={`/${org}/${p}`}
              className="rounded bg-amber-50 px-1 text-amber-900 underline"
            >
              {pages[p] ?? "a page"}
            </Link>
          ))}
        </div>
      )}
      <div className="flex gap-3 text-xs text-neutral-600">
        {threadHref && (
          <Link href={threadHref} className="underline">
            {message.replies > 0
              ? `${message.replies} ${message.replies === 1 ? "reply" : "replies"}`
              : "Reply"}
          </Link>
        )}
        <Link
          href={`/${org}/tasks?message=${message.id}`}
          className="underline"
        >
          Tasks
        </Link>
      </div>
    </li>
  );
}
