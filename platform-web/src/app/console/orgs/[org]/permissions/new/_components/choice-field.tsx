import { FormField } from "@garam/design/components/form-field";

/** One option of a `ChoiceField`, under the heading of its group if any. */
export type Choice = { value: string; label: string; group?: string };

/**
 * One labelled choice among `choices`, grouped by kind, submitted under
 * `name`. A native select: it is a form control the browser submits and
 * a test drives without a portal.
 */
export function ChoiceField({
  label,
  name,
  value,
  onChange,
  choices,
  empty,
}: {
  label: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  choices: readonly Choice[];
  /** What is said where there is nothing to choose from. */
  empty: string;
}) {
  const groups = [...new Set(choices.map((choice) => choice.group))];
  const option = (choice: Choice) => (
    <option key={choice.value} value={choice.value}>
      {choice.label}
    </option>
  );
  return (
    <FormField label={label} hint={choices.length === 0 ? empty : undefined}>
      {(control) => (
        <select
          {...control}
          name={name}
          required
          value={value}
          onChange={(event) => onChange(event.target.value)}
          className="h-8 w-full max-w-sm rounded-lg border border-input bg-background px-2.5 text-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm"
        >
          <option value="">Choose…</option>
          {groups.map((group) =>
            group === undefined ? (
              choices.filter((choice) => choice.group === undefined).map(option)
            ) : (
              <optgroup key={group} label={group}>
                {choices.filter((choice) => choice.group === group).map(option)}
              </optgroup>
            ),
          )}
        </select>
      )}
    </FormField>
  );
}
