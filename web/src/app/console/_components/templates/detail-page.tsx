import { CopyField } from "@/components/copy-field";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { DangerZone, type DangerAction } from "./danger-zone";

/** One tab of a detail page. */
export type DetailTab = {
  value: string;
  label: string;
  content: React.ReactNode;
};

/**
 * A page for one object: its header — name, copyable id in mono, status
 * and actions — then its tabs, with the danger zone closing the last
 * tab. The id is never the title: the name is, and the id is what a
 * visitor copies. An inactive tab is hidden rather than unmounted.
 */
export function DetailPage({
  name,
  idLabel,
  id,
  status,
  actions,
  tabs,
  defaultTab,
  dangerZone,
}: {
  name: string;
  /** What the identifier is called, as its copy control names it. */
  idLabel: string;
  id: string;
  /** A `StatusDot` saying where the object stands. */
  status?: React.ReactNode;
  /** The object's actions menu. */
  actions?: React.ReactNode;
  tabs: readonly DetailTab[];
  /** The tab open first, where it names one; otherwise the first. */
  defaultTab?: string;
  dangerZone?: readonly DangerAction[];
}) {
  const last = tabs.at(-1)?.value;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
        <div className="flex min-w-0 flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="truncate text-xl tracking-tight">{name}</h1>
            {status}
          </div>
          <CopyField label={idLabel} value={id} hideLabel />
        </div>
        {actions && (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
      <Tabs
        defaultValue={
          tabs.some((tab) => tab.value === defaultTab)
            ? defaultTab
            : tabs[0]?.value
        }
        className="gap-6"
      >
        {/* Scrolls sideways where the tabs outrun a narrow screen, rather
            than widening the page; the padding keeps the active tab's
            underline inside the scroll box. */}
        <div className="overflow-x-auto border-b pb-0.5 [scrollbar-width:none]">
          <TabsList variant="line" className="w-max min-w-full justify-start">
            {tabs.map((tab) => (
              <TabsTrigger
                key={tab.value}
                value={tab.value}
                className="flex-none px-2"
              >
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        {/* `forceMount` keeps what a person typed in a panel, a refusal
            and an inline outcome included, across a switch; it also
            leaves an inactive panel unhidden, so its state hides it. */}
        {tabs.map((tab) => (
          <TabsContent
            key={tab.value}
            value={tab.value}
            forceMount
            className="flex flex-col gap-6 data-[state=inactive]:hidden"
          >
            {tab.content}
            {tab.value === last && dangerZone && dangerZone.length > 0 && (
              <DangerZone actions={dangerZone} />
            )}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}
