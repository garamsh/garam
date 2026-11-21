"use client";

import { FormField } from "@garam/design/components/form-field";
import { Textarea } from "@garam/design/ui/textarea";

/**
 * Where the Cedar text of a guardrail or a permit is written: a plain
 * monospaced field, which submits as `text`. Cedar is parsed by the
 * policy service alone, so the editor checks nothing; what the service
 * refuses is `error`, shown at the field with the service's message.
 * The field is controlled so that the reset React runs after a refused
 * action leaves the text typed in it.
 */
export function CedarEditor({
  value,
  onChange,
  error,
}: {
  value: string;
  onChange: (text: string) => void;
  error?: string;
}) {
  return (
    <FormField
      label="Cedar text"
      hint="One or more Cedar policies. The policy service checks them when you save."
      error={error}
    >
      {(control) => (
        <Textarea
          {...control}
          name="text"
          required
          rows={12}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          className="font-mono"
        />
      )}
    </FormField>
  );
}
