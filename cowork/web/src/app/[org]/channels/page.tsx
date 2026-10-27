import Link from "next/link";

import {
  NewChannelForm,
  NewDirectForm,
} from "@/features/messaging/components/channel-forms";
import { listChannels } from "@/lib/cowork";
import { listParticipants, me } from "@/lib/platform";

/** The channels the caller may read, and the forms to start one. */
export default async function ChannelsPage({
  params,
}: {
  params: Promise<{ org: string }>;
}) {
  const { org } = await params;
  const [channels, participants, viewer] = await Promise.all([
    listChannels(org),
    listParticipants(org),
    me(),
  ]);
  const names = Object.fromEntries(participants.map((p) => [p.grn, p.name]));
  const others = participants.filter((p) => p.grn !== viewer?.grn);
  return (
    <section className="mx-auto flex max-w-3xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">Channels</h1>
      {channels.ok ? (
        <ul className="flex flex-col gap-1" aria-label="Channels">
          {channels.value.map((c) => (
            <li key={c.id}>
              <Link href={`/${org}/channels/${c.id}`} className="underline">
                {c.kind === "direct"
                  ? c.members
                      .filter((m) => m !== viewer?.grn)
                      .map((m) => names[m] ?? m)
                      .join(", ")
                  : `${c.kind === "private" ? "🔒 " : "#"}${c.name}`}
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <p role="alert" className="text-red-700">
          {channels.message}
        </p>
      )}
      <NewChannelForm org={org} />
      <NewDirectForm
        org={org}
        people={others.filter((p) => p.kind === "person")}
      />
    </section>
  );
}
