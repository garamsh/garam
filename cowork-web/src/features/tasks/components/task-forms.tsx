"use client";

import { useActionState } from "react";

import type { Task, TaskStatus } from "@/lib/cowork";
import type { Participant } from "@/lib/platform";

import { STATUS_LABEL } from "../status";

import {
  commentTaskAction,
  createTaskAction,
  moveTaskAction,
  updateTaskAction,
} from "../actions";

const field = "rounded border border-neutral-300 px-2 py-1";
const button = "rounded bg-neutral-900 px-2 py-1 text-white";

function Refusal({ error }: { error?: string }) {
  return error ? (
    <p role="alert" className="text-sm text-red-700">
      {error}
    </p>
  ) : null;
}

function AssigneeSelect({
  people,
  value,
}: {
  people: Participant[];
  value?: string;
}) {
  return (
    <select
      name="assignee"
      aria-label="Assignee"
      defaultValue={value ?? ""}
      className={field}
    >
      <option value="">No one</option>
      {people.map((p) => (
        <option key={p.grn} value={p.grn}>
          {p.name}
          {p.kind === "agent" ? " (agent)" : ""}
        </option>
      ))}
    </select>
  );
}

/**
 * A new task. Created from a page or a message, it links that page or
 * message.
 */
export function NewTaskForm({
  org,
  people,
  page,
  message,
}: {
  org: string;
  people: Participant[];
  page?: string;
  message?: string;
}) {
  const [state, action, pending] = useActionState(
    createTaskAction.bind(null, org),
    {},
  );
  return (
    <form
      action={action}
      aria-label="New task"
      className="flex flex-wrap items-center gap-2"
    >
      <input
        name="title"
        required
        aria-label="Task title"
        placeholder="New task"
        className={field}
      />
      <AssigneeSelect people={people} />
      <input name="due" type="date" aria-label="Due date" className={field} />
      {page && <input type="hidden" name="page" value={page} />}
      {message && <input type="hidden" name="message" value={message} />}
      <button type="submit" disabled={pending} className={button}>
        Add task
      </button>
      <Refusal error={state.error} />
    </form>
  );
}

/** Buttons that move a task to each status it is not in. */
export function MoveTask({ org, task }: { org: string; task: Task }) {
  const [state, action, pending] = useActionState(
    async (_prev: { error?: string }, status: TaskStatus) =>
      moveTaskAction(org, task.id, status),
    {},
  );
  return (
    <div className="flex flex-wrap gap-1 text-xs">
      {(Object.keys(STATUS_LABEL) as TaskStatus[])
        .filter((s) => s !== task.status)
        .map((s) => (
          <form key={s} action={() => action(s)}>
            <button
              type="submit"
              disabled={pending}
              aria-label={`Move ${task.title} to ${STATUS_LABEL[s]}`}
              className="rounded border border-neutral-300 px-1"
            >
              → {STATUS_LABEL[s]}
            </button>
          </form>
        ))}
      <Refusal error={state.error} />
    </div>
  );
}

/** A task's fields, saved together. */
export function EditTaskForm({
  org,
  task,
  people,
}: {
  org: string;
  task: Task;
  people: Participant[];
}) {
  const [state, action, pending] = useActionState(
    updateTaskAction.bind(null, org, task.id),
    {},
  );
  return (
    <form action={action} aria-label="Task" className="flex flex-col gap-2">
      <input
        name="title"
        required
        aria-label="Task title"
        defaultValue={task.title}
        className={`${field} text-xl font-semibold`}
      />
      <div className="flex flex-wrap gap-2">
        <select
          name="status"
          aria-label="Status"
          defaultValue={task.status}
          className={field}
        >
          {(Object.keys(STATUS_LABEL) as TaskStatus[]).map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <AssigneeSelect people={people} value={task.assignee} />
        <input
          name="due"
          type="date"
          aria-label="Due date"
          defaultValue={task.due}
          className={field}
        />
      </div>
      <textarea
        name="description"
        aria-label="Description"
        defaultValue={task.description}
        rows={4}
        className={field}
      />
      <div>
        <button type="submit" disabled={pending} className={button}>
          Save
        </button>
      </div>
      <Refusal error={state.error} />
    </form>
  );
}

export function CommentForm({ org, task }: { org: string; task: string }) {
  const [state, action, pending] = useActionState(
    commentTaskAction.bind(null, org, task),
    {},
  );
  return (
    <form action={action} aria-label="Comment" className="flex gap-2">
      <input
        name="body"
        required
        aria-label="Comment"
        placeholder="Comment"
        className={`${field} flex-1`}
      />
      <button type="submit" disabled={pending} className={button}>
        Comment
      </button>
      <Refusal error={state.error} />
    </form>
  );
}
