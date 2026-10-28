import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";

/**
 * The unreachable-API notice as a console page shows it: a region that
 * announces itself, under the `h1` the page's `PageHeader` already
 * holds. `subject` names the data the page cannot show.
 */
export function ApiUnreachable({ subject }: { subject: string }) {
  return (
    <section role="alert" className="flex flex-col gap-1">
      <h2 className="font-heading text-lg font-semibold">
        {API_UNREACHABLE_TITLE}
      </h2>
      <p className="text-sm text-muted-foreground">
        <ApiUnreachableReason subject={subject} />
      </p>
    </section>
  );
}
