"use client";

import { useActionState } from "react";

import type { Participant } from "@/lib/platform";

import {
  addMemberAction,
  createChannelAction,
  leaveChannelAction,
} from "../actions";

export function NewChannelForm({ org }: { org: string }) {
  const [state, action, pending] = useActionState(
    createChannelAction.bind(null, org),
    {},
  );
  return (
    <form
      action={action}
      className="flex flex-wrap items-center gap-2"
      aria-label="New channel"
    >
      <input
        name="name"
        required
        aria-label="Channel name"
        placeholder="New channel"
        className="rounded border border-neutral-300 px-2 py-1"
      />
      <select
        name="kind"
        aria-label="Who may join"
        className="rounded border border-neutral-300 px-1 py-1"
      >
        <option value="public">Public</option>
        <option value="private">Private</option>
      </select>
      <button
        type="submit"
        disabled={pending}
        className="rounded bg-neutral-900 px-2 py-1 text-white"
      >
        Create
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}

export function NewDirectForm({
  org,
  people,
}: {
  org: string;
  people: Participant[];
}) {
  const [state, action, pending] = useActionState(
    createChannelAction.bind(null, org),
    {},
  );
  return (
    <form
      action={action}
      className="flex flex-col gap-2"
      aria-label="New direct message"
    >
      <input type="hidden" name="kind" value="direct" />
      <fieldset className="flex flex-wrap gap-3 text-sm">
        <legend className="mb-1 font-semibold">Message directly</legend>
        {people.map((p) => (
          <label key={p.grn} className="flex items-center gap-1">
            <input type="checkbox" name="member" value={p.grn} />
            {p.name}
          </label>
        ))}
      </fieldset>
      <button
        type="submit"
        disabled={pending}
        className="self-start rounded bg-neutral-900 px-2 py-1 text-white"
      >
        Start
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}

export function AddMemberForm({
  org,
  channel,
  people,
}: {
  org: string;
  channel: string;
  people: Participant[];
}) {
  const [state, action, pending] = useActionState(
    addMemberAction.bind(null, org, channel),
    {},
  );
  return (
    <form action={action} className="flex items-center gap-2 text-sm">
      <select
        name="member"
        aria-label="Add a participant"
        className="rounded border border-neutral-300 px-1"
      >
        {people.map((p) => (
          <option key={p.grn} value={p.grn}>
            {p.name}
          </option>
        ))}
      </select>
      <button type="submit" disabled={pending} className="underline">
        Add
      </button>
      {state.error && (
        <p role="alert" className="text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}

export function LeaveButton({
  org,
  channel,
}: {
  org: string;
  channel: string;
}) {
  const [state, action, pending] = useActionState(
    leaveChannelAction.bind(null, org, channel),
    {},
  );
  return (
    <form action={action}>
      <button
        type="submit"
        disabled={pending}
        className="text-sm text-red-700 hover:underline"
      >
        Leave
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}
