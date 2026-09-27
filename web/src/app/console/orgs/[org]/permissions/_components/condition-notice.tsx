/**
 * Says that a document's Condition restricts nothing. garam stores it
 * and evaluates nothing from it, so a statement carrying one applies
 * as if it had none — the reverse of what a reader of the document
 * would assume.
 */
export function ConditionNotice() {
  return (
    <p role="note" className="text-sm text-destructive">
      This document has a Condition. garam does not evaluate Conditions, so the
      statement applies without it.
    </p>
  );
}
