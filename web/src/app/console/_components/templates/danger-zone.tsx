/** One irreversible action the danger zone offers. */
export type DangerAction = {
  title: string;
  /** What the action destroys or stops, in a sentence. */
  description: string;
  /** The control: a button opening an AlertDialog that names the act. */
  control: React.ReactNode;
};

/**
 * The card closing a detail page's settings with what cannot be undone.
 * Each action says what it costs beside its control, and the control
 * confirms in a dialog labelled with the act ("Delete operator"), never
 * a bare "OK".
 */
export function DangerZone({ actions }: { actions: readonly DangerAction[] }) {
  return (
    <section
      aria-labelledby="danger-zone-title"
      className="flex flex-col rounded-xl border border-danger/40"
    >
      <h2
        id="danger-zone-title"
        className="border-b border-danger/40 px-4 py-3 text-base text-danger"
      >
        Danger zone
      </h2>
      <ul className="flex flex-col">
        {actions.map((action) => (
          <li
            key={action.title}
            className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 last:border-b-0"
          >
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="font-medium">{action.title}</span>
              <span className="text-muted-foreground">
                {action.description}
              </span>
            </div>
            <div className="shrink-0">{action.control}</div>
          </li>
        ))}
      </ul>
    </section>
  );
}
