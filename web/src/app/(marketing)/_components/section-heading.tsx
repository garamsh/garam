/** The eyebrow, title and lead every landing section opens with. */
export function SectionHeading({
  eyebrow,
  title,
  body,
}: {
  eyebrow: string;
  title: string;
  body: string;
}) {
  return (
    <div className="flex max-w-2xl flex-col gap-4">
      <span className="text-eyebrow font-medium text-brand uppercase">
        {eyebrow}
      </span>
      <h2 className="font-heading text-title font-semibold text-balance">
        {title}
      </h2>
      <p className="text-lead text-pretty text-muted-foreground">{body}</p>
    </div>
  );
}
