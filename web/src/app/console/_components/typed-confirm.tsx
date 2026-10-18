"use client";

import { useId, useState } from "react";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/**
 * The field an act nothing undoes is confirmed in: the person types
 * `phrase`, and `onMatch` says whether what is typed is it. Both are
 * compared in one Unicode form, since Hangul stored decomposed (NFD) is
 * typed composed and would otherwise never match. `mono` shows an
 * identifier as one (ADR-0103 §Principles).
 */
export function TypedConfirm({
  phrase,
  mono = false,
  onMatch,
}: {
  phrase: string;
  mono?: boolean;
  onMatch: (matched: boolean) => void;
}) {
  const [typed, setTyped] = useState("");
  const id = useId();
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>
        {/* One span, so the label's gap does not split the sentence
            around the phrase; the phrase keeps its spaces as stored,
            so what is shown is what is typed. */}
        <span>
          Type{" "}
          <span
            className={cn(
              "whitespace-pre-wrap",
              mono ? "font-mono" : "font-semibold",
            )}
          >
            {phrase}
          </span>{" "}
          to confirm
        </span>
      </Label>
      <Input
        id={id}
        value={typed}
        onChange={(event) => {
          setTyped(event.target.value);
          onMatch(
            event.target.value.normalize("NFC") === phrase.normalize("NFC"),
          );
        }}
        autoComplete="off"
        spellCheck={false}
        className={cn(mono && "font-mono")}
      />
    </div>
  );
}
