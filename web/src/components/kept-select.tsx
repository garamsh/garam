"use client";

import { useLayoutEffect, useRef } from "react";

import { cn } from "@/lib/utils";

/**
 * A controlled `<select>` whose choice survives a refused submission.
 * Every console form holding a select renders it through this.
 *
 * React resets a form after its action, refusal included, by calling
 * `form.reset()`, which returns every field to its default. A controlled
 * `<input>` survives that because React keeps its `value` attribute,
 * which is the input's default, equal to its state. A controlled
 * `<select>` gets no such sync: its default is whichever option carries
 * `selected`, so the reset lands on the first option while the state
 * still holds the choice, and nothing re-renders to put it back.
 *
 * This marks the chosen option, and only it, as the default after every
 * commit, so the reset lands on the choice. The form keeps its action
 * and its progressive enhancement; only the select's default moves.
 *
 * It is drawn as the console's other controls are, 32px high; a caller
 * passes layout only.
 */
export function KeptSelect({
  value,
  className,
  ...props
}: Omit<React.ComponentProps<"select">, "value" | "defaultValue" | "ref"> & {
  value: string;
}) {
  const select = useRef<HTMLSelectElement>(null);
  // After every commit, not on a change of value alone: the options can
  // change under an unchanged choice.
  useLayoutEffect(() => {
    for (const option of select.current?.options ?? []) {
      option.defaultSelected = option.value === value;
    }
  });
  return (
    <select
      ref={select}
      value={value}
      className={cn(
        "h-8 rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive dark:bg-input/30",
        className,
      )}
      {...props}
    />
  );
}
