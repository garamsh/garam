/**
 * What a page shows in place of data whose read never reached the API;
 * `subject` names that data. The page renders this itself: a
 * production build replaces the message of an error thrown out of a
 * Server Component before `error.tsx` sees it, so a boundary cannot
 * name the API.
 */
export function ApiUnreachable({ subject }: { subject: string }) {
  return (
    <section role="alert" className="flex flex-col gap-1">
      <h2 className="font-heading text-lg font-semibold">
        Cannot reach the API
      </h2>
      <p className="text-sm text-muted-foreground">
        The garam API is not answering, so this page cannot show {subject}. This
        is not a sign-in problem and not a permission problem: try again once
        the API is back.
      </p>
    </section>
  );
}
