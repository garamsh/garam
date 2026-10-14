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
 * visitor copies.
 */
export function DetailPage({
  name,
  idLabel,
  id,
  status,
  actions,
  tabs,
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
      <Tabs defaultValue={tabs[0]?.value} className="gap-6">
        <TabsList variant="line" className="w-full justify-start border-b">
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
        {tabs.map((tab) => (
          <TabsContent
            key={tab.value}
            value={tab.value}
            className="flex flex-col gap-6"
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
