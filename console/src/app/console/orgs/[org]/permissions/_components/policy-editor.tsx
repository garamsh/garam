"use client";

import { useId, useState } from "react";

import { JsonEditor } from "@/components/json-editor";
import { Banner } from "@garam/design/components/banner";
import { Button } from "@garam/design/ui/button";
import { FieldDescription, FieldError } from "@garam/design/ui/field";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@garam/design/ui/tabs";
import {
  hasCondition,
  parsePolicyDocument,
  placeProblem,
  type DocumentProblem,
} from "@/lib/policy-document";

import { ConditionNotice } from "./condition-notice";
import { StructuredDocument } from "./structured-document";

type View = "statements" | "json";

/**
 * A policy document, edited as statements or as JSON — two views of
 * one text, which is what the form sends. A problem is said once,
 * under the editor, and marked where it is: on the statement control
 * it names, or underlined in the JSON. The JSON's own syntax is checked
 * as it is typed; everything else is the engine's to refuse, and
 * `problem` is that refusal, keyed on the place garam named when it
 * named one.
 */
export function PolicyEditor({
  value,
  onChange,
  problem,
  readOnly = false,
  hint,
}: {
  value: string;
  onChange: (value: string) => void;
  /** garam's refusal of this text, if it refused it. */
  problem: DocumentProblem | null;
  readOnly?: boolean;
  hint?: React.ReactNode;
}) {
  const id = useId();
  const labelId = `${id}-label`;
  const hintId = `${id}-hint`;
  const problemId = `${id}-problem`;
  const parsed = parsePolicyDocument(value);
  const message =
    problem?.message ?? (parsed.status === "error" ? parsed.message : null);
  const path = problem?.path;
  // Built from the two strings alone, so the compiler keeps one object
  // per problem rather than one per keystroke; the editor's marks
  // follow this, and a fresh object each render made them flicker.
  const shown: DocumentProblem | null =
    message === null ? null : { message, path };
  const place = shown === null ? null : placeProblem(shown.path);
  const [view, setView] = useState<View>(
    parsed.status === "ok" ? "statements" : "json",
  );
  const describedBy =
    [hint ? hintId : null, shown ? problemId : null]
      .filter(Boolean)
      .join(" ") || undefined;

  return (
    <div
      role="group"
      aria-labelledby={labelId}
      data-invalid={shown ? true : undefined}
      className="flex min-w-0 flex-col gap-2"
    >
      <Tabs
        value={view}
        onValueChange={(next) =>
          setView(next === "json" ? "json" : "statements")
        }
        className="gap-3"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span id={labelId} className="font-medium">
            Document
          </span>
          <TabsList aria-label="Document view">
            <TabsTrigger value="statements">Statements</TabsTrigger>
            <TabsTrigger value="json">JSON</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="statements">
          {parsed.status === "ok" ? (
            <StructuredDocument
              document={parsed.value}
              onChange={onChange}
              place={place}
              problemId={problemId}
              readOnly={readOnly}
              onOpenJson={() => setView("json")}
            />
          ) : (
            <Banner
              tone="info"
              title="Invalid JSON"
              action={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setView("json")}
                >
                  Edit as JSON
                </Button>
              }
            >
              Fix it in JSON.
            </Banner>
          )}
        </TabsContent>
        <TabsContent value="json">
          <JsonEditor
            value={value}
            onChange={onChange}
            label="Document JSON"
            invalid={shown !== null}
            describedBy={describedBy}
            problem={shown}
            readOnly={readOnly}
          />
        </TabsContent>
      </Tabs>
      {hint && <FieldDescription id={hintId}>{hint}</FieldDescription>}
      {shown && (
        <FieldError id={problemId} className="flex flex-col gap-0.5">
          {shown.message}
        </FieldError>
      )}
      {parsed.status === "ok" && hasCondition(parsed.value) && (
        <ConditionNotice />
      )}
    </div>
  );
}
