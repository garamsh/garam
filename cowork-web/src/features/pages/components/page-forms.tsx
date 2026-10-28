"use client";

import { useActionState } from "react";

import {
  createPageAction,
  deletePageAction,
  renamePageAction,
} from "../actions";

export function NewPageForm({
  org,
  parent = "",
}: {
  org: string;
  parent?: string;
}) {
  const [state, action, pending] = useActionState(
    createPageAction.bind(null, org, parent),
    {},
  );
  return (
    <form action={action} className="flex gap-2">
      <input
        name="title"
        aria-label={parent ? "New subpage title" : "New page title"}
        placeholder={parent ? "New subpage" : "New page"}
        className="min-w-0 flex-1 rounded border border-neutral-300 px-2 py-1 text-sm"
      />
      <button
        type="submit"
        disabled={pending}
        className="rounded bg-neutral-900 px-2 py-1 text-sm text-white"
      >
        Add
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}

export function RenamePageForm({
  org,
  id,
  title,
}: {
  org: string;
  id: string;
  title: string;
}) {
  const [state, action, pending] = useActionState(
    renamePageAction.bind(null, org, id),
    {},
  );
  return (
    <form action={action} className="flex items-center gap-2">
      <input
        name="title"
        defaultValue={title}
        aria-label="Page title"
        className="min-w-0 flex-1 border-b border-transparent text-2xl font-semibold focus:border-neutral-300 focus:outline-none"
      />
      <button
        type="submit"
        disabled={pending}
        className="text-sm text-neutral-500 hover:text-neutral-900"
      >
        Rename
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}

export function DeletePageButton({ org, id }: { org: string; id: string }) {
  const [state, action, pending] = useActionState(
    deletePageAction.bind(null, org, id),
    {},
  );
  return (
    <form action={action}>
      <button
        type="submit"
        disabled={pending}
        className="text-sm text-red-700 hover:underline"
      >
        Delete page
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}
