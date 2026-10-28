import { PageHeader } from "../page-header";

/** One section of a settings page, which saves on its own. */
export type SettingsSection = {
  id: string;
  title: string;
  description?: string;
  /** The section's form, ending in its own Save. */
  content: React.ReactNode;
};

/**
 * Settings split into sections, each saved by its own explicit Save so
 * that one section's change never rides on another's. A section nav
 * links them from `md` up.
 */
export function SettingsPage({
  title,
  description,
  sections,
  after,
}: {
  title: string;
  description?: string;
  sections: readonly SettingsSection[];
  /** What closes the page below the sections, such as a `DangerZone`. */
  after?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={title} description={description} />
      <div className="grid gap-8 md:grid-cols-[11rem_1fr]">
        <nav aria-label={`${title} sections`} className="hidden md:block">
          <ul className="sticky top-16 flex flex-col gap-0.5">
            {sections.map((section) => (
              <li key={section.id}>
                <a
                  href={`#${section.id}`}
                  className="block rounded-md px-2 py-1.5 text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
                >
                  {section.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <div className="flex min-w-0 flex-col gap-6">
          {sections.map((section) => (
            <section
              key={section.id}
              id={section.id}
              aria-labelledby={`${section.id}-title`}
              className="scroll-mt-16 rounded-xl border bg-card"
            >
              <div className="flex flex-col gap-1 border-b px-4 py-3">
                <h2 id={`${section.id}-title`} className="text-base">
                  {section.title}
                </h2>
                {section.description && (
                  <p className="text-muted-foreground">{section.description}</p>
                )}
              </div>
              <div className="p-4">{section.content}</div>
            </section>
          ))}
          {after}
        </div>
      </div>
    </div>
  );
}
