"use client";

import { Plus, X } from "lucide-react";
import { useId } from "react";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  formatPolicyDocument,
  readStatement,
  statementsOf,
  writeStatement,
  valueFor,
  withValue,
  type PolicyDocument,
  type ProblemPlace,
  type StatementFields,
  type StatementKey,
} from "@/lib/policy-document";
import { cn } from "@/lib/utils";

const EFFECTS = ["Allow", "Deny"] as const;

/** A statement as a new one starts: denying nothing yet named. */
const NEW_STATEMENT = { Effect: "Deny", Action: [""], Resource: [""] };

/**
 * The document's statements as controls: an Effect, an optional label,
 * and the Actions and Resources one per row. Every change rewrites the
 * JSON the form sends, so this view and the JSON view are one document;
 * keys this view has no control for, a Condition among them, are kept
 * as written and edited in JSON.
 *
 * Where a refusal named a place, the control there is marked invalid
 * and described by the refusal's sentence, `problemId`.
 */
export function StructuredDocument({
  document,
  onChange,
  place,
  problemId,
  readOnly,
  onOpenJson,
}: {
  document: PolicyDocument;
  onChange: (text: string) => void;
  place: ProblemPlace | null;
  problemId: string;
  readOnly: boolean;
  onOpenJson: () => void;
}) {
  const statements = statementsOf(document);
  const versionValue = valueFor(document, "Version");
  const version = typeof versionValue === "string" ? versionValue : "";
  const versionId = useId();
  const write = (next: PolicyDocument) => onChange(formatPolicyDocument(next));
  const withStatements = (next: unknown[]) =>
    withValue(document, "Statement", next);
  const invalid = (index: number, key?: StatementKey) =>
    place?.at === "statement" &&
    place.index === index &&
    (key === undefined ? place.key === undefined : place.key === key);

  if (statements === null) {
    return (
      <Banner
        tone="info"
        title="This document's Statement is not a list"
        action={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onOpenJson}
          >
            Edit as JSON
          </Button>
        }
      >
        Statement holds one statement per entry of a list. Fix it in JSON to
        edit statements here.
      </Banner>
    );
  }

  const setStatement = (
    index: number,
    fields: Omit<StatementFields, "conditioned">,
  ) =>
    write(
      withStatements(
        statements.map((statement, at) =>
          at === index ? writeStatement(statement, fields) : statement,
        ),
      ),
    );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex max-w-xs flex-col gap-1.5">
        <Label htmlFor={versionId}>Format version</Label>
        <Input
          id={versionId}
          value={version}
          readOnly={readOnly}
          aria-invalid={place?.at === "version" || undefined}
          aria-describedby={place?.at === "version" ? problemId : undefined}
          onChange={(event) =>
            write(withValue(document, "Version", event.target.value))
          }
          className="font-mono text-table"
        />
      </div>

      <ol className="flex flex-col gap-3" aria-label="Statements">
        {statements.map((statement, index) => {
          const fields = readStatement(statement);
          const name = `Statement ${index + 1}`;
          return (
            <li
              key={index}
              aria-label={name}
              aria-describedby={invalid(index) ? problemId : undefined}
              className={cn(
                "flex flex-col gap-3 rounded-xl border bg-card p-3 sm:p-4",
                invalid(index) && "border-destructive",
              )}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium">{name}</span>
                {!readOnly && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={`Remove ${name.toLowerCase()}`}
                    onClick={() =>
                      write(
                        withStatements(
                          statements.filter((_, at) => at !== index),
                        ),
                      )
                    }
                  >
                    <X aria-hidden />
                    Remove
                  </Button>
                )}
              </div>
              {fields === null ? (
                <p className="text-muted-foreground">
                  Only the JSON view edits this statement: it is not an object,
                  or its Action or Resource is not a list of strings, which
                  garam refuses.
                </p>
              ) : (
                <StatementControls
                  name={name}
                  fields={fields}
                  readOnly={readOnly}
                  invalid={(key) => invalid(index, key)}
                  problemId={problemId}
                  onChange={(next) => setStatement(index, next)}
                />
              )}
            </li>
          );
        })}
      </ol>
      {statements.length === 0 && (
        <p className="text-muted-foreground">
          No statements yet. A policy needs at least one.
        </p>
      )}
      {!readOnly && (
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              write(withStatements([...statements, NEW_STATEMENT]))
            }
          >
            <Plus aria-hidden />
            Add statement
          </Button>
        </div>
      )}
    </div>
  );
}

/** One statement's Effect, label, Actions and Resources. */
function StatementControls({
  name,
  fields,
  readOnly,
  invalid,
  problemId,
  onChange,
}: {
  name: string;
  fields: StatementFields;
  readOnly: boolean;
  invalid: (key: StatementKey) => boolean;
  problemId: string;
  onChange: (fields: Omit<StatementFields, "conditioned">) => void;
}) {
  const effectId = useId();
  const sidId = useId();
  const described = (key: StatementKey) => ({
    "aria-invalid": invalid(key) || undefined,
    "aria-describedby": invalid(key) ? problemId : undefined,
  });
  const known = EFFECTS.some((effect) => effect === fields.effect);

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-[9rem_1fr]">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={effectId}>Effect</Label>
          <Select
            value={fields.effect}
            disabled={readOnly}
            onValueChange={(effect) => {
              // A form reset answers with the empty default, not a
              // choice.
              if (effect !== "") onChange({ ...fields, effect });
            }}
          >
            <SelectTrigger
              id={effectId}
              aria-label={`${name} effect`}
              {...described("Effect")}
              className="w-full"
            >
              <SelectValue placeholder="Choose" />
            </SelectTrigger>
            <SelectContent position="popper">
              {EFFECTS.map((effect) => (
                <SelectItem key={effect} value={effect}>
                  {effect}
                </SelectItem>
              ))}
              {!known && fields.effect !== "" && (
                <SelectItem value={fields.effect} disabled>
                  {fields.effect} (not valid)
                </SelectItem>
              )}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor={sidId}>
            Label{" "}
            <span className="font-normal text-muted-foreground">
              (Sid, optional)
            </span>
          </Label>
          <Input
            id={sidId}
            value={fields.sid}
            readOnly={readOnly}
            aria-label={`${name} label`}
            {...described("Sid")}
            onChange={(event) =>
              onChange({ ...fields, sid: event.target.value })
            }
          />
        </div>
      </div>
      <ValueList
        label="Actions"
        itemLabel={`${name} action`}
        addLabel="Add action"
        placeholder="project:update"
        values={fields.actions}
        readOnly={readOnly}
        invalid={invalid("Action")}
        problemId={problemId}
        onChange={(actions) => onChange({ ...fields, actions })}
      />
      <ValueList
        label="Resources"
        itemLabel={`${name} resource`}
        addLabel="Add resource"
        placeholder="grn:org:default:project:*"
        values={fields.resources}
        readOnly={readOnly}
        invalid={invalid("Resource")}
        problemId={problemId}
        onChange={(resources) => onChange({ ...fields, resources })}
      />
      {fields.conditioned && (
        <p className="text-sm text-muted-foreground">
          Has a Condition, kept as written. Edit it in JSON.
        </p>
      )}
    </div>
  );
}

/** A list of strings, one control per entry, added and removed by row. */
function ValueList({
  label,
  itemLabel,
  addLabel,
  placeholder,
  values,
  readOnly,
  invalid,
  problemId,
  onChange,
}: {
  label: string;
  itemLabel: string;
  addLabel: string;
  placeholder: string;
  values: string[];
  readOnly: boolean;
  invalid: boolean;
  problemId: string;
  onChange: (values: string[]) => void;
}) {
  const labelId = useId();
  return (
    <div
      role="group"
      aria-labelledby={labelId}
      aria-describedby={invalid ? problemId : undefined}
      className="flex flex-col gap-1.5"
    >
      <span id={labelId} className="text-sm font-medium">
        {label}
      </span>
      <ul className="flex flex-col gap-1.5">
        {values.map((value, index) => (
          <li key={index} className="flex items-center gap-1.5">
            <Input
              value={value}
              readOnly={readOnly}
              placeholder={placeholder}
              aria-label={`${itemLabel} ${index + 1}`}
              aria-invalid={invalid || undefined}
              onChange={(event) =>
                onChange(
                  values.map((item, at) =>
                    at === index ? event.target.value : item,
                  ),
                )
              }
              className="font-mono text-table"
            />
            {!readOnly && (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`Remove ${itemLabel} ${index + 1}`}
                onClick={() => onChange(values.filter((_, at) => at !== index))}
              >
                <X aria-hidden />
              </Button>
            )}
          </li>
        ))}
      </ul>
      {values.length === 0 && (
        <p
          className={cn(
            "text-sm text-muted-foreground",
            invalid && "text-destructive",
          )}
        >
          None yet.
        </p>
      )}
      {!readOnly && (
        <div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => onChange([...values, ""])}
          >
            <Plus aria-hidden />
            {addLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
