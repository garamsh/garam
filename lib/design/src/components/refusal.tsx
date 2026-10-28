/**
 * A refusal said beside the control or form that met it: a sentence the
 * console wrote, announced as an alert. A refusal that belongs to one
 * field is that field's error instead.
 */
export function Refusal({ children }: { children: React.ReactNode }) {
  return (
    <p role="alert" className="text-danger">
      {children}
    </p>
  );
}
