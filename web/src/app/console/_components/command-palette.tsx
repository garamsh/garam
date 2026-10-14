"use client";

import { Building2, Monitor, Moon, Plus, Search, Sun } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";

import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Kbd } from "@/components/ui/kbd";
import { useTheme } from "@/hooks/use-theme";

import {
  CONSOLE_NAV_GROUPS,
  orgHref,
  parseConsolePath,
  resolveNavEntry,
  type ConsoleOrg,
} from "./navigation";

/** Whether a key press landed where it is text being typed. */
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
 * anywhere, and `/` from anywhere text is not being typed. It lists the
 * sections of the organization on screen, the caller's organizations,
 * and the theme.
 */
export function CommandPalette({ orgs }: { orgs: readonly ConsoleOrg[] }) {
  const [open, setOpen] = useState(false);
  const router = useRouter();
  const location = parseConsolePath(usePathname());
  const { setTheme } = useTheme();
  const chordLabel = useChordLabel();

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
      event.preventDefault();
      setOpen((wasOpen) => (chord ? !wasOpen : true));
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  function run(action: () => void) {
    setOpen(false);
    action();
  }

  const org = location.kind === "org" ? location.org : null;
  const sections = org
    ? [{ section: "" }, ...CONSOLE_NAV_GROUPS.flatMap((group) => group.entries)]
        .map(resolveNavEntry)
        .filter((entry) => entry !== undefined)
    : [];

  return (
    <>
      <Button
        type="button"
        variant="outline"
        onClick={() => setOpen(true)}
        aria-keyshortcuts="Meta+K Control+K /"
        className="h-8 gap-2 px-2 text-muted-foreground sm:w-56 sm:justify-start"
      >
        <Search />
        <span className="sr-only sm:not-sr-only">Search or jump to…</span>
        <Kbd className="ml-auto hidden sm:inline-flex">{chordLabel}</Kbd>
      </Button>
      {/* Composed here rather than with the CLI's CommandDialog, which
          renders its title outside the dialog and so leaves a heading
          on every page while the palette is closed. */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          showCloseButton={false}
          className="top-[20%] translate-y-0 overflow-hidden p-0 sm:max-w-lg"
        >
          <DialogTitle className="sr-only">Search or jump to</DialogTitle>
          <DialogDescription className="sr-only">
            Go to a section, switch organization, or change the theme.
          </DialogDescription>
          <Command>
            <CommandInput placeholder="Search or jump to…" />
            <CommandList>
              <CommandEmpty>Nothing matches that.</CommandEmpty>
              {org && sections.length > 0 && (
                <CommandGroup heading="Go to">
                  {sections.map((entry) => (
                    <CommandItem
                      key={entry.key}
                      value={`go ${entry.label}`}
                      onSelect={() =>
                        run(() => router.push(orgHref(org, entry.segment)))
                      }
                    >
                      <entry.icon />
                      {entry.label}
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
              <CommandGroup heading="Organizations">
                {orgs.map((candidate) => (
                  <CommandItem
                    key={candidate.slug}
                    value={`org ${candidate.name} ${candidate.slug}`}
                    onSelect={() =>
                      run(() => router.push(orgHref(candidate.slug, "")))
                    }
                  >
                    <Building2 />
                    {candidate.name}
                  </CommandItem>
                ))}
                <CommandItem
                  value="org new organization create"
                  onSelect={() => run(() => router.push("/console/orgs/new"))}
                >
                  <Plus />
                  New organization
                </CommandItem>
              </CommandGroup>
              <CommandGroup heading="Theme">
                <CommandItem
                  value="theme system"
                  onSelect={() => run(() => setTheme("system"))}
                >
                  <Monitor />
                  Use system theme
                </CommandItem>
                <CommandItem
                  value="theme light"
                  onSelect={() => run(() => setTheme("light"))}
                >
                  <Sun />
                  Use light theme
                </CommandItem>
                <CommandItem
                  value="theme dark"
                  onSelect={() => run(() => setTheme("dark"))}
                >
                  <Moon />
                  Use dark theme
                </CommandItem>
              </CommandGroup>
            </CommandList>
          </Command>
        </DialogContent>
      </Dialog>
    </>
  );
}
