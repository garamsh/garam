"use client";

import { useEffect, useId, useRef, useSyncExternalStore } from "react";

/**
 * An action the page on screen offers, as the palette lists it: what
 * it does and, in one line, what follows from it.
 */
export type PageAction = {
  id: string;
  label: string;
  consequence: string;
  /** Opens the page's own control for it. */
  run: () => void;
};

// The palette renders in the shell, above every page, so a store
// carries the page's actions up to it for the reason `org-shell.tsx`
// gives for its own.
let actions: readonly PageAction[] = [];
const listeners = new Set<() => void>();

function publish(next: readonly PageAction[]) {
  actions = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Lists an action in the palette while the calling control is
 * mounted. `run` may change between renders; the latest is called.
 */
export function usePageAction(
  command: { label: string; consequence: string } | undefined,
  run: () => void,
) {
  const id = useId();
  const latest = useRef(run);
  useEffect(() => {
    latest.current = run;
  });
  const label = command?.label;
  const consequence = command?.consequence;
  useEffect(() => {
    if (label === undefined || consequence === undefined) return;
    publish([
      ...actions,
      { id, label, consequence, run: () => latest.current() },
    ]);
    return () => publish(actions.filter((action) => action.id !== id));
  }, [id, label, consequence]);
}

/** The actions the page on screen lists, in the order it mounted them. */
export function usePageActions(): readonly PageAction[] {
  return useSyncExternalStore(
    subscribe,
    () => actions,
    () => EMPTY,
  );
}

const EMPTY: readonly PageAction[] = [];
