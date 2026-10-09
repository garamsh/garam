"use client";

import { useLayoutEffect, useRef } from "react";

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
 */
export function KeptSelect({
  value,
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
  return <select ref={select} value={value} {...props} />;
}
