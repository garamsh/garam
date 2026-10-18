"use client";

import { Command as CommandPrimitive } from "cmdk";
import {
  Building2,
  FolderGit2,
  Monitor,
  Moon,
  Plus,
  Search,
  ShieldCheck,
  Sun,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useRef, useState } from "react";

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Kbd } from "@/components/ui/kbd";
import { MemberMark } from "@/components/member-mark";
import { useTheme } from "@/hooks/use-theme";

import { CONSOLE_NAV_ENTRIES, orgHref, type ConsoleOrg } from "./navigation";
import { useOrgShell, type PaletteObject } from "./org-shell";
import { usePageActions } from "./page-actions";

/** The palette's groups of objects, in the order they are listed. */
const OBJECT_GROUPS: readonly {
  kind: PaletteObject["kind"];
  heading: string;
}[] = [
  { kind: "person", heading: "People" },
  { kind: "agent", heading: "Agents" },
  { kind: "team", heading: "Teams" },
  { kind: "policy", heading: "Policies" },
  { kind: "project", heading: "Projects" },
];

/** An object's mark: a member's shape, or its section's icon. */
function ObjectMark({ object }: { object: PaletteObject }) {
  switch (object.kind) {
    case "policy":
      return <ShieldCheck />;
    case "project":
      return <FolderGit2 />;
    default:
      return (
        <MemberMark kind={object.kind} name={object.name} className="size-5" />
      );
  }
}

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
  actionsOnly,
  org,
  orgs,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Lists the page's actions and nothing else. */
  actionsOnly: boolean;
  /** The organization on screen, when the caller holds it. */
  org: string | null;
  orgs: readonly ConsoleOrg[] | null;
}) {
  const router = useRouter();
  const { setTheme } = useTheme();
  const shell = useOrgShell();
  const actions = usePageActions();
  const [search, setSearch] = useState("");
  // A page action opens its own dialog once this one has closed, so
  // the two never hold focus at once.
  const deferred = useRef<(() => void) | null>(null);

  function run(action: () => void) {
    onOpenChange(false);
    action();
  }

  const hidden = new Set(shell?.hidden);
  const sections = org
    ? CONSOLE_NAV_ENTRIES.filter((entry) => !hidden.has(entry.key))
    : [];
  // Objects are found, not browsed: an organization lists too many to
  // scroll, so they are offered once something is typed.
  const objects = org && search.trim() ? (shell?.objects ?? []) : [];

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-foreground/10 data-[state=open]:animate-in data-[state=open]:fade-in-0 dark:bg-black/40" />
        <DialogPrimitive.Content
          data-slot="command-palette"
          onCloseAutoFocus={(event) => {
            const action = deferred.current;
            deferred.current = null;
            if (action) {
              event.preventDefault();
              action();
            }
          }}
          className="fixed top-[15%] left-1/2 z-50 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 overflow-hidden rounded-xl bg-popover text-popover-foreground shadow-lg ring-1 ring-foreground/10 outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
        >
          <DialogPrimitive.Title className="sr-only">
            {actionsOnly ? "Actions on this page" : "Search or jump to"}
          </DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            {actionsOnly
              ? "Choose what to do here; each says what follows from it."
              : "Find a person, agent, team, policy or project, go to a section, act on this page, switch organization, or change the theme."}
          </DialogPrimitive.Description>
          <Command filter={paletteFilter} className="rounded-none! p-0">
            <div className="flex items-center gap-2 border-b px-4">
              <Search aria-hidden className="size-4 text-muted-foreground" />
              <CommandPrimitive.Input
                value={search}
                onValueChange={setSearch}
                placeholder={
                  actionsOnly
                    ? "Actions on this page…"
                    : "Find a person, agent, team or policy…"
                }
                className="h-12 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-0"
              />
            </div>
            <CommandList className="max-h-80 p-1.5">
              <CommandEmpty>
                {actionsOnly && actions.length === 0
                  ? "This page offers no actions here."
                  : "Nothing matches that."}
              </CommandEmpty>
              {actions.length > 0 && (
                <CommandGroup heading="Actions on this page">
                  {actions.map((action) => (
                    <CommandItem
                      key={action.id}
                      value={`action ${action.id}`}
                      keywords={[action.label]}
                      onSelect={() => {
                        deferred.current = action.run;
                        onOpenChange(false);
                      }}
                      className="flex-col items-start gap-0.5"
                    >
                      <span>{action.label}</span>
                      <span className="text-xs text-muted-foreground">
                        {action.consequence}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
              {!actionsOnly &&
                org &&
                OBJECT_GROUPS.map(({ kind, heading }) => {
                  const found = objects.filter(
                    (object) => object.kind === kind,
                  );
                  return (
                    found.length > 0 && (
                      <CommandGroup key={kind} heading={heading}>
                        {found.map((object) => (
                          <CommandItem
                            key={`${kind} ${object.identifier}`}
                            value={`${kind} ${object.identifier}`}
                            keywords={[object.name, object.identifier]}
                            onSelect={() =>
                              run(() =>
                                router.push(orgHref(org, object.segment)),
                              )
                            }
                          >
                            <ObjectMark object={object} />
                            <span className="truncate">{object.name}</span>
                            <span className="ml-auto font-mono text-xs text-muted-foreground">
                              {object.identifier}
                            </span>
                          </CommandItem>
                        ))}
                      </CommandGroup>
                    )
                  );
                })}
              {!actionsOnly && sections.length > 0 && org && (
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
              {!actionsOnly && (
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
              )}
              {!actionsOnly && (
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
              )}
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
