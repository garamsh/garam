import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";

import type { ChoiceGroup } from "../../_components/attach-policy-form";

/**
 * Chooses the team, person or agent whose policies the view shows. A
 * plain GET form, so the choice is an address that can be linked and
 * bookmarked, carrying the subject's kind and identifier rather than
 * its GRN.
 */
export function SubjectPicker({
  action,
  groups,
  current,
}: {
  action: string;
  groups: ChoiceGroup[];
  current: string;
}) {
  const offered = groups.filter((group) => group.options.length > 0);
  if (offered.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No team, person or agent is listed to you.
      </p>
    );
  }
  return (
    <form
      method="get"
      action={action}
      className="flex max-w-2xl flex-col gap-3 sm:flex-row sm:items-end"
    >
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <Label htmlFor="subject">Team, person or agent</Label>
        <select
          id="subject"
          name="subject"
          required
          defaultValue={current}
          className="h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
        >
          <option value="" disabled>
            Choose whose policies to show
          </option>
          {offered.map((group) => (
            <optgroup key={group.label} label={group.label}>
              {group.options.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
      </div>
      <Button type="submit" variant="outline">
        Show policies
      </Button>
    </form>
  );
}
