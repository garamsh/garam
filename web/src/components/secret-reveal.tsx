"use client";

import { Check, Copy, Download } from "lucide-react";
import { useId, useState } from "react";

import { Refusal } from "@/components/refusal";
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

type SecretRevealProps = {
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
};

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
export function SecretReveal(props: SecretRevealProps) {
  // Mounted only while open, and keyed by the secret: whether one was
  // copied, downloaded or acknowledged never carries over to the next,
  // and nothing about it stays in state once the dialog closes.
  if (!props.open) return null;
  return <OpenSecretReveal key={props.secret} {...props} />;
}

function OpenSecretReveal({
  title,
  description,
  secret,
  noun,
  filename,
  onDone,
}: SecretRevealProps) {
  const acknowledgeId = useId();
  const { outcome, copy } = useCopy(secret);
  const [downloaded, setDownloaded] = useState<boolean | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);
  const kept = outcome === "copied" || downloaded === true || acknowledged;

  // Counts as kept only once the file was handed to the browser: a
  // throw on the way leaves the dialog shut to dismissal.
  function download() {
    if (!filename) return;
    let href: string | undefined;
    try {
      href = URL.createObjectURL(
        new Blob([secret], { type: "application/octet-stream" }),
      );
      const anchor = document.createElement("a");
      anchor.href = href;
      anchor.download = filename;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      setDownloaded(true);
    } catch {
      setDownloaded(false);
    } finally {
      // Revoked after the click's task, which a browser may still be
      // reading the object URL in.
      const created = href;
      if (created) setTimeout(() => URL.revokeObjectURL(created));
    }
  }

  return (
    <AlertDialog open>
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
        <pre className="max-h-48 overflow-auto rounded-lg border bg-muted px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap select-all">
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
        {downloaded === false && (
          <Refusal>Download failed. Copy it instead.</Refusal>
        )}
        {outcome === "failed" && (
          <Refusal>Copy failed. Select it and copy it yourself.</Refusal>
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
            I have stored the {noun}. It won&apos;t be shown again.
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
