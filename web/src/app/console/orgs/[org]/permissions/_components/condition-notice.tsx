import { Banner } from "@/components/banner";

/**
 * Says that a document's Condition restricts nothing. garam stores it
 * and evaluates nothing from it, so a statement carrying one applies
 * as if it had none — the reverse of what a reader of the document
 * would assume.
 */
export function ConditionNotice() {
  return (
    <Banner tone="warn" title="Conditions are not evaluated">
      This document has a Condition. garam stores it but does not evaluate it,
      so the statement applies as if it had none.
    </Banner>
  );
}
