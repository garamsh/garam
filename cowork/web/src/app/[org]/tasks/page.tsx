import Link from "next/link";

import { NewTaskForm } from "@/features/tasks/components/task-forms";
import { TaskBoard, TaskList } from "@/features/tasks/components/task-views";
import { listTasks } from "@/lib/cowork";
import { listParticipants } from "@/lib/platform";

/**
 * The workspace's tasks, as a list or a board. Opened from a page or a
 * message, it lists the tasks that link it, and a task added here
 * links it too.
 */
export default async function TasksPage({
  params,
  searchParams,
}: {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ view?: string; page?: string; message?: string }>;
}) {
  const { org } = await params;
  const { view, page, message } = await searchParams;
  const [tasks, participants] = await Promise.all([
    listTasks(org, { page, message }),
    listParticipants(org),
  ]);
  const names = Object.fromEntries(participants.map((p) => [p.grn, p.name]));
  const board = view === "board";
  const keep = new URLSearchParams(
    Object.entries({ page, message }).filter(
      (e): e is [string, string] => !!e[1],
    ),
  );
  const href = (v: string) => {
    const q = new URLSearchParams(keep);
    if (v === "board") q.set("view", "board");
    const s = q.toString();
    return `/${org}/tasks${s ? `?${s}` : ""}`;
  };
  return (
    <section className="mx-auto flex max-w-5xl flex-col gap-6">
      <div className="flex items-baseline justify-between gap-4">
        <h1 className="text-2xl font-semibold">
          {page || message ? "Linked tasks" : "Tasks"}
        </h1>
        <nav aria-label="Task views" className="flex gap-3 text-sm">
          <Link
            href={href("list")}
            aria-current={board ? undefined : "page"}
            className="underline"
          >
            List
          </Link>
          <Link
            href={href("board")}
            aria-current={board ? "page" : undefined}
            className="underline"
          >
            Board
          </Link>
        </nav>
      </div>
      <NewTaskForm
        org={org}
        people={participants}
        page={page}
        message={message}
      />
      {!tasks.ok ? (
        <p role="alert" className="text-red-700">
          {tasks.message}
        </p>
      ) : board ? (
        <TaskBoard org={org} tasks={tasks.value} names={names} />
      ) : (
        <TaskList org={org} tasks={tasks.value} names={names} />
      )}
    </section>
  );
}
