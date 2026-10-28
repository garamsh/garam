import Link from "next/link";
import { notFound } from "next/navigation";

import {
  CommentForm,
  EditTaskForm,
} from "@/features/tasks/components/task-forms";
import {
  getPage,
  getTask,
  getThread,
  listTaskComments,
  type Message,
} from "@/lib/cowork";
import { listParticipants } from "@/lib/platform";

/** A task: its fields, what it links, and its comments. */
export default async function TaskView({
  params,
}: {
  params: Promise<{ org: string; task: string }>;
}) {
  const { org, task } = await params;
  const [t, comments, participants] = await Promise.all([
    getTask(org, task),
    listTaskComments(org, task),
    listParticipants(org),
  ]);
  if (!t.ok && t.status === 404) notFound();
  if (!t.ok) {
    return (
      <p role="alert" className="text-red-700">
        {t.message}
      </p>
    );
  }
  const names = Object.fromEntries(participants.map((p) => [p.grn, p.name]));
  // A link is followed as its reader may: one the reader may not read
  // is shown without what it says.
  const [pages, messages] = await Promise.all([
    Promise.all(t.value.pages.map((id) => getPage(org, id))),
    Promise.all(
      t.value.messages.map(async (id) => {
        const thread = await getThread(org, id);
        return thread.ok ? thread.value.find((m) => m.id === id) : undefined;
      }),
    ),
  ]);
  return (
    <article className="mx-auto flex max-w-3xl flex-col gap-6">
      <Link href={`/${org}/tasks`} className="text-sm underline">
        ← Tasks
      </Link>
      <EditTaskForm
        key={t.value.updatedAt}
        org={org}
        task={t.value}
        people={participants}
      />
      {(t.value.pages.length > 0 || t.value.messages.length > 0) && (
        <section aria-label="Links" className="flex flex-col gap-1 text-sm">
          <h2 className="font-semibold">Links</h2>
          <ul className="flex flex-col gap-1">
            {t.value.pages.map((id, i) => {
              const p = pages[i];
              return (
                <li key={id}>
                  <Link href={`/${org}/${id}`} className="underline">
                    {p?.ok ? p.value.title : "A page you may not read"}
                  </Link>
                </li>
              );
            })}
            {t.value.messages.map((id, i) => {
              const m: Message | undefined = messages[i];
              return (
                <li key={id}>
                  {m ? (
                    <Link
                      href={`/${org}/channels/${m.channel}?thread=${m.thread || m.id}`}
                      className="underline"
                    >
                      {m.body.length > 80 ? `${m.body.slice(0, 80)}…` : m.body}
                    </Link>
                  ) : (
                    "A message you may not read"
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      )}
      <section aria-label="Comments" className="flex flex-col gap-2">
        <h2 className="font-semibold">Comments</h2>
        {comments.ok && (
          <ul className="flex flex-col gap-2">
            {comments.value.map((c) => (
              <li key={c.id} className="flex flex-col">
                <span className="text-sm font-semibold">
                  {names[c.author] ?? c.author.split(":").at(-1)}
                </span>
                <p className="whitespace-pre-wrap">{c.body}</p>
              </li>
            ))}
          </ul>
        )}
        <CommentForm org={org} task={task} />
      </section>
    </article>
  );
}
