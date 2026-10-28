"use client";

import { useEffect, useRef, useState } from "react";

import type { JsonEditorHandle } from "@/lib/codemirror";
import { cn } from "@garam/design/lib/utils";

/** What the editable element says about itself to assistive technology. */
function contentAttributes(
  label: string,
  invalid: boolean,
  describedBy: string | undefined,
  readOnly: boolean,
): Record<string, string> {
  return {
    "aria-label": label,
    "aria-multiline": "true",
    ...(invalid ? { "aria-invalid": "true" } : {}),
    ...(describedBy ? { "aria-describedby": describedBy } : {}),
    ...(readOnly ? { "aria-readonly": "true" } : {}),
  };
}

/**
 * A JSON document edited as code: line numbers, bracket matching, a
 * syntax error marked where it is, and a refusal's JSON Pointer marked
 * at the place it names. CodeMirror is loaded in the browser only, so
 * the server renders the text as it stands and the editor takes over
 * once it has loaded; until then nothing can be typed.
 *
 * The text is the caller's state: what is typed reaches `onChange`, and
 * a `value` changed elsewhere — by a structured view of the same
 * document — replaces the text in the editor.
 */
export function JsonEditor({
  value,
  onChange,
  label,
  invalid = false,
  describedBy,
  problem,
  readOnly = false,
  placeholder,
  className,
}: {
  value: string;
  onChange?: (value: string) => void;
  /** The control's accessible name. */
  label: string;
  invalid?: boolean;
  describedBy?: string;
  /** What to mark in the text: a refusal and the place it names. */
  problem?: { message: string; path?: string } | null;
  readOnly?: boolean;
  placeholder?: string;
  className?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<JsonEditorHandle | null>(null);
  const [ready, setReady] = useState(false);
  // The latest props, read by the editor's callbacks and by the mount
  // that finishes after a render has moved them on.
  const latest = useRef({ value, onChange });
  useEffect(() => {
    latest.current = { value, onChange };
  });

  useEffect(() => {
    let cancelled = false;
    void import("@/lib/codemirror").then(({ mountJsonEditor }) => {
      if (cancelled || host.current === null) return;
      editor.current = mountJsonEditor({
        parent: host.current,
        doc: latest.current.value,
        attributes: contentAttributes(label, invalid, describedBy, readOnly),
        readOnly,
        placeholder,
        onChange: (doc) => latest.current.onChange?.(doc),
      });
      setReady(true);
    });
    return () => {
      cancelled = true;
      editor.current?.destroy();
      editor.current = null;
      setReady(false);
    };
    // The editor is built once per mode; later props reach it below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readOnly]);

  useEffect(() => {
    editor.current?.setDoc(value);
  }, [value, ready]);

  useEffect(() => {
    editor.current?.setAttributes(
      contentAttributes(label, invalid, describedBy, readOnly),
    );
  }, [label, invalid, describedBy, readOnly, ready]);

  // Keyed on what the problem says, so an equal one leaves the mark;
  // `ready` marks the mounted editor once.
  const problemMessage = problem?.message;
  const problemPath = problem?.path;
  useEffect(() => {
    editor.current?.setProblem(
      problemMessage === undefined
        ? null
        : { message: problemMessage, path: problemPath },
    );
  }, [problemMessage, problemPath, ready]);

  return (
    <div
      data-slot="json-editor"
      aria-busy={!ready}
      aria-invalid={invalid || undefined}
      className={cn(
        "relative min-h-40 overflow-hidden rounded-lg border border-input bg-background transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 aria-invalid:border-destructive dark:bg-input/30",
        readOnly && "bg-muted/40 dark:bg-muted/30",
        className,
      )}
    >
      {!ready && (
        // What the server rendered: the text, read-only, until the
        // editor replaces it in the same place.
        <pre className="m-0 overflow-x-auto py-2 pr-3 pl-10 font-mono text-table leading-[1.6] whitespace-pre-wrap text-muted-foreground">
          {value}
        </pre>
      )}
      <div ref={host} className={cn(!ready && "hidden")} />
    </div>
  );
}
