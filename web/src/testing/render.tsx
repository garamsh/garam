import { render, type RenderOptions } from "@testing-library/react";

/**
 * Renders an async Server Component: awaits the element it returns and
 * hands the resolved tree to Testing Library.
 */
export async function renderServerComponent(
  element: Promise<React.ReactElement>,
  options?: RenderOptions,
) {
  return render(await element, options);
}
