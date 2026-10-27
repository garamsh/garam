import Link from "next/link";

import { TASK_STATUSES, type Task } from "@/lib/cowork";

import { STATUS_LABEL } from "../status";
import { MoveTask } from "./task-forms";

export type Names = Record<string, string>;

const nameOf = (names: Names, grn: string) =>
  grn ? (names[grn] ?? grn.split(":").at(-1) ?? grn) : "";

function Meta({ task, names }: { task: Task; names: Names }) {
  return (
    <span className="text-xs text-neutral-600">
      {[nameOf(names, task.assignee), task.due && `due ${task.due}`]
        .filter(Boolean)
        .join(" · ")}
    </span>
  );
}

/** Tasks as a list, newest first. */
export function TaskList({
  org,
  tasks,
  names,
}: {
  org: string;
  tasks: Task[];
  names: Names;
}) {
  if (tasks.length === 0) return <p className="text-neutral-600">No tasks.</p>;
  return (
    <ul aria-label="Tasks" className="flex flex-col gap-2">
      {tasks.map((t) => (
        <li
          key={t.id}
          data-testid="task"
          className="flex flex-wrap items-baseline gap-2"
        >
          <span className="rounded bg-neutral-100 px-1 text-xs">
            {STATUS_LABEL[t.status]}
          </span>
          <Link href={`/${org}/tasks/${t.id}`} className="underline">
            {t.title}
          </Link>
          <Meta task={t} names={names} />
        </li>
      ))}
    </ul>
  );
}

/** Tasks as a board: a column per status, each card movable. */
export function TaskBoard({
  org,
  tasks,
  names,
}: {
  org: string;
  tasks: Task[];
  names: Names;
}) {
  return (
    <div className="grid grid-cols-3 gap-4">
      {TASK_STATUSES.map((s) => (
        <section
          key={s}
          aria-label={STATUS_LABEL[s]}
          className="flex flex-col gap-2 rounded bg-neutral-50 p-2"
        >
          <h2 className="font-semibold">{STATUS_LABEL[s]}</h2>
          {tasks
            .filter((t) => t.status === s)
            .map((t) => (
              <article
                key={t.id}
                data-testid="task-card"
                className="flex flex-col gap-1 rounded border border-neutral-200 bg-white p-2"
              >
                <Link href={`/${org}/tasks/${t.id}`} className="underline">
                  {t.title}
                </Link>
                <Meta task={t} names={names} />
                <MoveTask org={org} task={t} />
              </article>
            ))}
        </section>
      ))}
    </div>
  );
}
