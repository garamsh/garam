import { FormField } from "@garam/design/components/form-field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@garam/design/ui/select";

/** One option of a `ChoiceField`, under the heading of its group. */
export type Choice = { value: string; label: string; group?: string };

/**
 * One labelled choice among `choices`, grouped by kind, and sent from
 * a hidden input named `name`, so the form posts before hydration as
 * after it (as the add-member form does). The select answers the
 * form's reset with its empty default, which is no choice and is
 * ignored, so a refused choice stays chosen.
 */
export function ChoiceField({
  label,
  name,
  value,
  onChange,
  choices,
  unlisted,
  empty,
}: {
  label: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  choices: readonly Choice[];
  /** The kinds whose list garam refused the caller, as plural nouns. */
  unlisted: readonly string[];
  /** What is said where nothing was refused and nothing is offered. */
  empty: string;
}) {
  const groups = [...new Set(choices.map((choice) => choice.group))];
  const hint =
    unlisted.length > 0
      ? `This organization's ${unlisted.join(" and ")} are not listed to you, so none of them can be chosen here.`
      : choices.length === 0
        ? empty
        : undefined;
  const item = (choice: Choice) => (
    <SelectItem key={choice.value} value={choice.value}>
      {choice.label}
    </SelectItem>
  );
  return (
    <FormField label={label} hint={hint}>
      {(control) => (
        <>
          <input type="hidden" name={name} value={value} />
          <Select
            value={value}
            onValueChange={(next) => {
              // The form's reset answers with the empty default; no
              // option is empty, so that is not a choice.
              if (next !== "") onChange(next);
            }}
          >
            <SelectTrigger {...control} className="w-full max-w-sm">
              <SelectValue placeholder="Choose…" />
            </SelectTrigger>
            <SelectContent>
              {groups.map((group) =>
                group === undefined ? (
                  choices
                    .filter((choice) => choice.group === undefined)
                    .map(item)
                ) : (
                  <SelectGroup key={group}>
                    <SelectLabel>{group}</SelectLabel>
                    {choices
                      .filter((choice) => choice.group === group)
                      .map(item)}
                  </SelectGroup>
                ),
              )}
            </SelectContent>
          </Select>
        </>
      )}
    </FormField>
  );
}
