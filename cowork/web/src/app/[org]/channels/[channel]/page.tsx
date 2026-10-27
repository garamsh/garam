import Link from "next/link";
import { notFound } from "next/navigation";

import { env } from "@/env";
import {
  AddMemberForm,
  LeaveButton,
} from "@/features/messaging/components/channel-forms";
import { Composer } from "@/features/messaging/components/composer";
import { LiveMessages } from "@/features/messaging/components/live-messages";
import {
  getChannel,
  getThread,
  listArtifacts,
  listMessages,
  listPages,
} from "@/lib/cowork";
import { listParticipants, me } from "@/lib/platform";

/**
 * One channel: its messages as they arrive, a composer, and the thread
 * `?thread=` names beside them.
 */
export default async function ChannelPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string; channel: string }>;
  searchParams: Promise<{ thread?: string }>;
}) {
  const [{ org, channel }, { thread }] = await Promise.all([
    params,
    searchParams,
  ]);
  const [ch, messages, participants, pages, viewer, artifacts] =
    await Promise.all([
      getChannel(org, channel),
      listMessages(org, channel),
      listParticipants(org),
      listPages(org),
      me(),
      listArtifacts(org),
    ]);
  // A server without a blob store answers no artifacts; the composer
  // then offers none.
  const artifactList = artifacts.ok
    ? artifacts.value.map((a) => ({ id: a.id, title: a.title }))
    : [];
  if (!ch.ok && ch.status === 404) notFound();
  if (!ch.ok || !messages.ok) {
    return (
      <p role="alert" className="text-red-700">
        {!ch.ok ? ch.message : !messages.ok ? messages.message : ""}
      </p>
    );
  }
  const threadMessages = thread ? await getThread(org, thread) : null;
  const names = Object.fromEntries(participants.map((p) => [p.grn, p.name]));
  const pageList = pages.ok
    ? pages.value.map((p) => ({ id: p.id, title: p.title }))
    : [];
  const titles = Object.fromEntries(
    pageList.map((p) => [p.id, p.title || "Untitled"]),
  );
  const title =
    ch.value.kind === "direct"
      ? ch.value.members
          .filter((m) => m !== viewer?.grn)
          .map((m) => names[m] ?? m)
          .join(", ")
      : ch.value.name;
  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
      <section className="flex flex-col gap-4">
        <header className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold">
            {ch.value.kind === "public" ? "#" : ""}
            {title}
          </h1>
          {ch.value.kind === "private" && (
            <AddMemberForm
              org={org}
              channel={channel}
              people={participants.filter(
                (p) => !ch.value.members.includes(p.grn),
              )}
            />
          )}
          {ch.value.kind !== "public" && (
            <LeaveButton org={org} channel={channel} />
          )}
        </header>
        <LiveMessages
          org={org}
          channel={channel}
          initial={messages.value}
          syncUrl={env.COWORK_SYNC_URL}
          names={names}
          pages={titles}
        />
        <Composer
          org={org}
          channel={channel}
          participants={participants}
          pages={pageList}
          artifacts={artifactList}
        />
      </section>
      {thread && (
        <aside
          className="flex flex-col gap-4 border-l border-neutral-200 pl-4"
          aria-label="Thread panel"
        >
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">Thread</h2>
            <Link
              href={`/${org}/channels/${channel}`}
              className="text-sm underline"
            >
              Close
            </Link>
          </div>
          {threadMessages?.ok ? (
            <>
              <LiveMessages
                key={thread}
                org={org}
                channel={channel}
                thread={thread}
                initial={threadMessages.value}
                syncUrl={env.COWORK_SYNC_URL}
                names={names}
                pages={titles}
              />
              <Composer
                org={org}
                channel={channel}
                thread={thread}
                participants={participants}
                pages={pageList}
                artifacts={artifactList}
              />
            </>
          ) : (
            <p role="alert" className="text-red-700">
              {threadMessages?.ok === false ? threadMessages.message : ""}
            </p>
          )}
        </aside>
      )}
    </div>
  );
}
