"use client";

import { Check, Copy, Download } from "lucide-react";
import { useId, useState } from "react";

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useCopy } from "@/hooks/use-copy";

/**
 * A secret garam answers once — a key, a credential, an enrollment
 * token — shown in a dialog that cannot be dismissed until the secret
 * has been copied, downloaded or acknowledged as stored. Escape and a
 * click outside do nothing before then.
 *
 * The caller holds the secret in the client state a Server Action
 * returned to, and nowhere else: never in a URL, a toast, or data a
 * revalidation would render again. Download builds the file in the
 * browser from that state, so the secret never becomes a link.
 */
export function SecretReveal({
  open,
  title,
  description,
  secret,
  noun,
  filename,
  onDone,
}: {
  open: boolean;
  title: string;
  description: React.ReactNode;
  secret: string;
  /** What the secret is, in the sentence the acknowledgement reads. */
  noun: string;
  /** Offers a download under this name when present. */
  filename?: string;
  /** Called once the visitor has kept the secret and closes the dialog. */
  onDone: () => void;
}) {
  const acknowledgeId = useId();
  const { outcome, copy } = useCopy(secret);
  const [downloaded, setDownloaded] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const kept = outcome === "copied" || downloaded || acknowledged;

  function download() {
    if (!filename) return;
    const href = URL.createObjectURL(
      new Blob([secret], { type: "application/octet-stream" }),
    );
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(href);
    setDownloaded(true);
  }

  return (
    <AlertDialog open={open}>
      <AlertDialogContent
        className="data-[size=default]:max-w-[calc(100%-2rem)] data-[size=default]:sm:max-w-lg"
        onEscapeKeyDown={(event) => {
          if (!kept) event.preventDefault();
          else onDone();
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <pre
          aria-label={noun}
          className="max-h-48 overflow-auto rounded-lg border bg-muted px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap select-all"
        >
          {secret}
        </pre>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" onClick={copy}>
            {outcome === "copied" ? <Check className="text-ok" /> : <Copy />}
            {outcome === "copied" ? "Copied" : "Copy"}
          </Button>
          {filename && (
            <Button type="button" variant="outline" onClick={download}>
              {downloaded ? <Check className="text-ok" /> : <Download />}
              {downloaded ? "Downloaded" : "Download"}
            </Button>
          )}
        </div>
        {outcome === "failed" && (
          <p role="alert" className="text-destructive">
            Could not copy the {noun}. Select it above and copy it yourself.
          </p>
        )}
        <div className="flex items-start gap-2">
          <input
            id={acknowledgeId}
            type="checkbox"
            checked={acknowledged}
            onChange={(event) => setAcknowledged(event.target.checked)}
            className="mt-0.5 size-4 accent-primary"
          />
          <label htmlFor={acknowledgeId}>
            I have stored the {noun} somewhere safe. It will not be shown again.
          </label>
        </div>
        <AlertDialogFooter>
          <Button type="button" disabled={!kept} onClick={onDone}>
            Done
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
