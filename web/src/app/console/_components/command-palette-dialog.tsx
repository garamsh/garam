"use client";

import { Command as CommandPrimitive } from "cmdk";
import { Building2, Monitor, Moon, Plus, Search, Sun } from "lucide-react";
import { useRouter } from "next/navigation";
import { Dialog as DialogPrimitive } from "radix-ui";

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Kbd } from "@/components/ui/kbd";
import { useTheme } from "@/hooks/use-theme";

import {
  CONSOLE_NAV_GROUPS,
  orgHref,
  resolveNavEntry,
  type ConsoleOrg,
} from "./navigation";

/**
 * Matches an entry when the query appears in one of its words as typed,
 * case aside: "temp" finds Templates, and "tmpl" finds nothing. cmdk's
 * own scoring matches any scattered letters, which lists most of the
 * console for a two-letter query.
 */
export function paletteFilter(
  _value: string,
  search: string,
  keywords?: string[],
): number {
  const query = search.trim().toLowerCase();
  if (!query) return 1;
  return (keywords ?? []).some((keyword) =>
    keyword.toLowerCase().includes(query),
  )
    ? 1
    : 0;
}

/**
 * The palette itself, loaded the first time it opens so cmdk ships with
 * no page. Composed from the Radix dialog rather than the CLI's
 * CommandDialog, which renders its title outside the dialog and frames
 * its input as a field.
 */
export function CommandPaletteDialog({
  open,
  onOpenChange,
  org,
  orgs,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The organization on screen, when the caller holds it. */
  org: string | null;
  orgs: readonly ConsoleOrg[] | null;
}) {
  const router = useRouter();
  const { setTheme } = useTheme();

  function run(action: () => void) {
    onOpenChange(false);
    action();
  }

  const sections = org
    ? [{ section: "" }, ...CONSOLE_NAV_GROUPS.flatMap((group) => group.entries)]
        .map(resolveNavEntry)
        .filter((entry) => entry !== undefined)
    : [];

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-foreground/10 data-[state=open]:animate-in data-[state=open]:fade-in-0 dark:bg-black/40" />
        <DialogPrimitive.Content
          data-slot="command-palette"
          className="fixed top-[15%] left-1/2 z-50 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 overflow-hidden rounded-xl bg-popover text-popover-foreground shadow-lg ring-1 ring-foreground/10 outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
        >
          <DialogPrimitive.Title className="sr-only">
            Search or jump to
          </DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            Go to a section, switch organization, or change the theme.
          </DialogPrimitive.Description>
          <Command filter={paletteFilter} className="rounded-none! p-0">
            <div className="flex items-center gap-2 border-b px-4">
              <Search aria-hidden className="size-4 text-muted-foreground" />
              <CommandPrimitive.Input
                placeholder="Search or jump to…"
                className="h-12 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-0"
              />
            </div>
            <CommandList className="max-h-80 p-1.5">
              <CommandEmpty>Nothing matches that.</CommandEmpty>
              {sections.length > 0 && org && (
                <CommandGroup heading="Go to">
                  {sections.map((entry) => (
                    <CommandItem
                      key={entry.key}
                      value={`section ${entry.key}`}
                      keywords={[entry.label]}
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
                {(orgs ?? []).map((candidate) => (
                  <CommandItem
                    key={candidate.slug}
                    value={`org ${candidate.slug}`}
                    keywords={[candidate.name, candidate.slug]}
                    onSelect={() =>
                      run(() => router.push(orgHref(candidate.slug, "")))
                    }
                  >
                    <Building2 />
                    {candidate.name}
                  </CommandItem>
                ))}
                <CommandItem
                  value="org-new"
                  keywords={["New organization", "create"]}
                  onSelect={() => run(() => router.push("/console/orgs/new"))}
                >
                  <Plus />
                  New organization
                </CommandItem>
              </CommandGroup>
              <CommandGroup heading="Theme">
                <CommandItem
                  value="theme-system"
                  keywords={["Use system theme"]}
                  onSelect={() => run(() => setTheme("system"))}
                >
                  <Monitor />
                  Use system theme
                </CommandItem>
                <CommandItem
                  value="theme-light"
                  keywords={["Use light theme"]}
                  onSelect={() => run(() => setTheme("light"))}
                >
                  <Sun />
                  Use light theme
                </CommandItem>
                <CommandItem
                  value="theme-dark"
                  keywords={["Use dark theme"]}
                  onSelect={() => run(() => setTheme("dark"))}
                >
                  <Moon />
                  Use dark theme
                </CommandItem>
              </CommandGroup>
            </CommandList>
            <div className="flex items-center gap-4 border-t px-4 py-2 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <Kbd>↵</Kbd> to open
              </span>
              <span className="flex items-center gap-1.5">
                <Kbd>↑</Kbd>
                <Kbd>↓</Kbd> to move
              </span>
              <span className="flex items-center gap-1.5">
                <Kbd>esc</Kbd> to close
              </span>
            </div>
          </Command>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
