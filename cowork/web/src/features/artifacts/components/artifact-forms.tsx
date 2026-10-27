"use client";

import { useActionState, useState } from "react";

import { createArtifactAction, saveVersionAction } from "../actions";

const field = "rounded border border-neutral-300 px-2 py-1";
const button = "rounded bg-neutral-900 px-2 py-1 text-white";

function Refusal({ error }: { error?: string }) {
  return error ? (
    <p role="alert" className="text-sm text-red-700">
      {error}
    </p>
  ) : null;
}

/** A new artifact: an HTML page, or a page frozen as a document. */
export function NewArtifactForm({
  org,
  pages,
}: {
  org: string;
  pages: { id: string; title: string }[];
}) {
  const [kind, setKind] = useState<"html" | "document">("html");
  const [state, action, pending] = useActionState(
    createArtifactAction.bind(null, org),
    {},
  );
  return (
    <form
      action={action}
      aria-label="New artifact"
      className="flex flex-col gap-2"
    >
      <div className="flex flex-wrap gap-2">
        <input
          name="title"
          required
          aria-label="Artifact title"
          placeholder="New artifact"
          className={field}
        />
        <select
          name="kind"
          aria-label="Kind"
          value={kind}
          onChange={(e) => setKind(e.target.value as "html" | "document")}
          className={field}
        >
          <option value="html">HTML</option>
          <option value="document">Document from a page</option>
        </select>
        {kind === "document" && (
          <select name="page" aria-label="Page" className={field} required>
            {pages.map((p) => (
              <option key={p.id} value={p.id}>
                {p.title || "Untitled"}
              </option>
            ))}
          </select>
        )}
      </div>
      {kind === "html" && (
        <textarea
          name="html"
          required
          aria-label="HTML"
          rows={6}
          className={`${field} font-mono text-sm`}
        />
      )}
      <div>
        <button type="submit" disabled={pending} className={button}>
          Create
        </button>
      </div>
      <Refusal error={state.error} />
    </form>
  );
}

/** Saves the next version: new HTML, or the page frozen again. */
export function SaveVersionForm({
  org,
  id,
  kind,
  page,
}: {
  org: string;
  id: string;
  kind: "html" | "document";
  page?: string;
}) {
  const [state, action, pending] = useActionState(
    saveVersionAction.bind(null, org, id, kind),
    {},
  );
  return (
    <form
      action={action}
      aria-label="New version"
      className="flex flex-col gap-2"
    >
      {kind === "html" ? (
        <textarea
          name="html"
          required
          aria-label="HTML"
          rows={6}
          className={`${field} font-mono text-sm`}
        />
      ) : (
        <input type="hidden" name="page" value={page ?? ""} />
      )}
      <div>
        <button type="submit" disabled={pending} className={button}>
          {kind === "html" ? "Save a new version" : "Freeze the page again"}
        </button>
      </div>
      <Refusal error={state.error} />
    </form>
  );
}
