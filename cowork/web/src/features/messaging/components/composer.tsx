"use client";

import { useActionState } from "react";

import type { Participant } from "@/lib/platform";

import { postMessageAction } from "../actions";

type Props = {
  org: string;
  channel: string;
  thread?: string;
  participants: Participant[];
  pages: { id: string; title: string }[];
  /** Artifacts the writer may embed. */
  artifacts?: { id: string; title: string }[];
};

/**
 * Writes a message: its text, whom it mentions, and a page it links.
 * A sent message clears the form; a refused one keeps it and says why.
 */
export function Composer({
  org,
  channel,
  thread = "",
  participants,
  pages,
  artifacts = [],
}: Props) {
  const [state, action, pending] = useActionState(
    postMessageAction.bind(null, org, channel, thread),
    {},
  );
  return (
    <form
      key={state.sent ?? 0}
      action={action}
      className="flex flex-col gap-2 border-t border-neutral-200 pt-3"
    >
      <textarea
        name="body"
        required
        rows={3}
        aria-label={thread ? "Reply" : "Message"}
        placeholder={thread ? "Reply in thread" : "Write a message"}
        className="rounded border border-neutral-300 p-2"
      />
      <div className="flex flex-wrap items-center gap-3 text-sm">
        {participants.length > 0 && (
          <details>
            <summary className="cursor-pointer">Mention</summary>
            <fieldset className="mt-1 flex flex-col gap-1">
              <legend className="sr-only">Mention</legend>
              {participants.map((p) => (
                <label key={p.grn} className="flex items-center gap-1">
                  <input type="checkbox" name="mention" value={p.grn} />
                  {p.name}
                  {p.kind === "agent" && (
                    <span className="text-xs text-neutral-500">(agent)</span>
                  )}
                </label>
              ))}
            </fieldset>
          </details>
        )}
        <label className="flex items-center gap-1">
          Link a page
          <select
            name="page"
            className="rounded border border-neutral-300 px-1"
            defaultValue=""
          >
            <option value="">None</option>
            {pages.map((p) => (
              <option key={p.id} value={p.id}>
                {p.title || "Untitled"}
              </option>
            ))}
          </select>
        </label>
        {artifacts.length > 0 && (
          <label className="flex items-center gap-1">
            Embed an artifact
            <select
              name="artifact"
              className="rounded border border-neutral-300 px-1"
              defaultValue=""
            >
              <option value="">None</option>
              {artifacts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.title}
                </option>
              ))}
            </select>
          </label>
        )}
        <button
          type="submit"
          disabled={pending}
          className="ml-auto rounded bg-neutral-900 px-3 py-1 text-white"
        >
          Send
        </button>
      </div>
      {state.error && (
        <p role="alert" className="text-sm text-red-700">
          {state.error}
        </p>
      )}
    </form>
  );
}
