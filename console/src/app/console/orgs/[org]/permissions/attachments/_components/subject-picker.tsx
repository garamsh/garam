"use client";

import { useState } from "react";

import { Button } from "@garam/design/ui/button";
import { Label } from "@garam/design/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@garam/design/ui/select";

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
  const [chosen, setChosen] = useState(current);
  const offered = groups.filter((group) => group.options.length > 0);
  if (offered.length === 0) {
    return (
      <p className="text-muted-foreground">
        No team, person or agent is listed to you.
      </p>
    );
  }
  return (
    <form
      method="get"
      action={action}
      className="flex flex-col gap-3 sm:flex-row sm:items-end"
    >
      <input type="hidden" name="subject" value={chosen} />
      <div className="flex min-w-0 flex-1 flex-col gap-1.5 sm:max-w-md">
        <Label htmlFor="subject">Team, person or agent</Label>
        <Select
          value={chosen}
          onValueChange={(value) => {
            // A form reset answers with the empty default, not a
            // choice.
            if (value !== "") setChosen(value);
          }}
        >
          <SelectTrigger id="subject" className="w-full min-w-0">
            <SelectValue placeholder="Choose whose policies to show" />
          </SelectTrigger>
          <SelectContent position="popper">
            {offered.map((group) => (
              <SelectGroup key={group.label}>
                <SelectLabel>{group.label}</SelectLabel>
                {group.options.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Button type="submit" variant="outline" disabled={chosen === ""}>
        Show policies
      </Button>
    </form>
  );
}
