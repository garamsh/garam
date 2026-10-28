import { useId } from "react";

import { Field, FieldDescription, FieldError, FieldLabel } from "../ui/field";

/** What a field's control is handed to wire itself to the label. */
export type FieldControlProps = {
  id: string;
  "aria-invalid": true | undefined;
  "aria-describedby": string | undefined;
};

/**
 * One form control with its label, an optional hint, and the error the
 * server returned for it. The control is rendered by the caller with
 * the props it is handed, so the label, the hint and the error are tied
 * to it — `aria-describedby` reaches both, and `aria-invalid` is set
 * exactly when there is an error to read.
 */
export function FormField({
  label,
  hint,
  error,
  children,
  className,
}: {
  label: string;
  hint?: React.ReactNode;
  /** The sentence the failed submission returned for this field. */
  error?: string;
  children: (control: FieldControlProps) => React.ReactNode;
  className?: string;
}) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy =
    [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(" ") ||
    undefined;

  return (
    <Field data-invalid={error ? true : undefined} className={className}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {children({
        id,
        "aria-invalid": error ? true : undefined,
        "aria-describedby": describedBy,
      })}
      {hint && <FieldDescription id={hintId}>{hint}</FieldDescription>}
      {error && <FieldError id={errorId}>{error}</FieldError>}
    </Field>
  );
}
