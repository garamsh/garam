"use client";

import { Search } from "lucide-react";
import dynamic from "next/dynamic";
import { usePathname } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";

import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { useHydrated } from "@/hooks/use-hydrated";

import { isUnknownOrg, parseConsolePath, type ConsoleOrg } from "./navigation";

const loadDialog = () =>
  import("./command-palette-dialog").then(
    (module) => module.CommandPaletteDialog,
  );

// cmdk and the dialog load on first open, not with every page.
const CommandPaletteDialog = dynamic(loadDialog, { ssr: false });

/** Whether a key press landed where text is being typed. */
function typing(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.isContentEditable ||
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement
  );
}

const noSubscription = () => () => {};

/** The chord as this platform spells it; the server assumes a Mac. */
function useChordLabel(): string {
  return useSyncExternalStore(
    noSubscription,
    () => (/Mac|iPhone|iPad/.test(navigator.platform) ? "⌘K" : "Ctrl K"),
    () => "⌘K",
  );
}

/**
 * Jumps anywhere in the console from the keyboard: ⌘K or Ctrl+K from
 * anywhere, and `/` from anywhere text is not being typed. Neither
 * opens it over an alert dialog, which holds the visitor until they
 * answer it — a secret shown once among them. It lists the sections of
 * the organization on screen, the caller's organizations, and the
 * theme. `orgs` is null when the list was not answered.
 */
export function CommandPalette({
  orgs,
}: {
  orgs: readonly ConsoleOrg[] | null;
}) {
  const [open, setOpen] = useState(false);
  const [requested, setRequested] = useState(false);
  const location = parseConsolePath(usePathname());
  const chordLabel = useChordLabel();
  const hydrated = useHydrated();

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const chord =
        event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey);
      const slash =
        event.key === "/" &&
        !event.metaKey &&
        !event.ctrlKey &&
        !event.altKey &&
        !typing(event.target);
      if (!chord && !slash) return;
      if (document.querySelector('[role="alertdialog"]')) return;
      event.preventDefault();
      setRequested(true);
      setOpen((wasOpen) => (chord ? !wasOpen : true));
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const org =
    location.kind === "org" && !isUnknownOrg(location, orgs)
      ? location.org
      : null;

  return (
    <>
      <Button
        type="button"
        variant="outline"
        // A click before hydration would be lost, so the control waits.
        disabled={!hydrated}
        onPointerEnter={() => void loadDialog()}
        onFocus={() => void loadDialog()}
        onClick={() => {
          setRequested(true);
          setOpen(true);
        }}
        aria-keyshortcuts="Meta+K Control+K /"
        className="gap-2 px-2 text-muted-foreground sm:w-56 sm:justify-start"
      >
        <Search />
        <span className="sr-only sm:not-sr-only">Search or jump to…</span>
        <Kbd className="ml-auto hidden sm:inline-flex">{chordLabel}</Kbd>
      </Button>
      {requested && (
        <CommandPaletteDialog
          open={open}
          onOpenChange={setOpen}
          org={org}
          orgs={orgs}
        />
      )}
    </>
  );
}
