import { Stepper, type Step } from "@/components/stepper";

import { PageHeader } from "../page-header";

/**
 * A task done in steps: the stepper down the side, the step in hand
 * beside it. The wizard's progress lives in what the server holds, so
 * `completed` comes from a read and a visitor returning resumes where
 * they stopped; the last step verifies the result.
 */
export function WizardPage({
  title,
  description,
  steps,
  current,
  completed,
  children,
}: {
  title: string;
  description?: string;
  steps: readonly Step[];
  current: string;
  completed?: readonly string[];
  /** The current step's content. */
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={title} description={description} />
      <div className="grid gap-8 md:grid-cols-[14rem_1fr]">
        <Stepper
          label={`${title} steps`}
          steps={steps}
          current={current}
          completed={completed}
        />
        <section
          aria-label={steps.find((step) => step.id === current)?.title}
          className="min-w-0 rounded-xl border bg-card p-4 md:p-6"
        >
          {children}
        </section>
      </div>
    </div>
  );
}
