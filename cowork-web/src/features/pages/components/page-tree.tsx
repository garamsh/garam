import Link from "next/link";

import type { Page } from "@/lib/cowork";

type Props = { org: string; pages: Page[]; current?: string };

/**
 * The page tree as nested lists: a page's children under it, siblings
 * by position. A page whose parent is not listed is not shown, since
 * the caller may not read the parent.
 */
export function PageTree({ org, pages, current }: Props) {
  const children = (parent: string) =>
    pages
      .filter((p) => p.parent === parent)
      .sort((a, b) => a.position - b.position);
  const render = (parent: string) => {
    const level = children(parent);
    if (level.length === 0) return null;
    return (
      <ul className="ml-3 flex flex-col gap-1">
        {level.map((p) => (
          <li key={p.id}>
            <Link
              href={`/${org}/${p.id}`}
              aria-current={p.id === current ? "page" : undefined}
              className="block truncate rounded px-1 hover:bg-neutral-100 aria-[current=page]:font-semibold"
            >
              {p.title || "Untitled"}
            </Link>
            {render(p.id)}
          </li>
        ))}
      </ul>
    );
  };
  return (
    <nav aria-label="Pages" className="text-sm">
      {render("") ?? <p className="text-neutral-500">No pages yet.</p>}
    </nav>
  );
}
