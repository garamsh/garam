import { MessageItem } from "@/features/messaging/components/message-item";
import { listPages, searchMessages } from "@/lib/cowork";
import { listParticipants } from "@/lib/platform";

/** Messages matching `?q=` in every channel the caller may read. */
export default async function SearchPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ q?: string }>;
}) {
  const [{ org }, { q = "" }] = await Promise.all([params, searchParams]);
  const [found, participants, pages] = await Promise.all([
    q ? searchMessages(org, q) : Promise.resolve(null),
    listParticipants(org),
    listPages(org),
  ]);
  const names = Object.fromEntries(participants.map((p) => [p.grn, p.name]));
  const titles = pages.ok
    ? Object.fromEntries(pages.value.map((p) => [p.id, p.title || "Untitled"]))
    : {};
  return (
    <section className="mx-auto flex max-w-3xl flex-col gap-4">
      <form className="flex gap-2" role="search">
        <input
          name="q"
          defaultValue={q}
          aria-label="Search messages"
          placeholder="Search messages"
          className="flex-1 rounded border border-neutral-300 px-2 py-1"
        />
        <button
          type="submit"
          className="rounded bg-neutral-900 px-3 py-1 text-white"
        >
          Search
        </button>
      </form>
      {found && !found.ok && (
        <p role="alert" className="text-red-700">
          {found.message}
        </p>
      )}
      {found?.ok && (
        <ol className="flex flex-col gap-4" aria-label="Results">
          {found.value.length === 0 && (
            <li className="text-neutral-500">No message matches.</li>
          )}
          {found.value.map((m) => (
            <MessageItem
              key={m.id}
              org={org}
              message={m}
              names={names}
              pages={titles}
              threadHref={`/${org}/channels/${m.channel}?thread=${m.thread || m.id}`}
            />
          ))}
        </ol>
      )}
    </section>
  );
}
