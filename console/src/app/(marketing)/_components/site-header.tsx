import Link from "next/link";

import { BrandMark } from "@garam/design/components/brand-mark";
import { Button } from "@garam/design/ui/button";

import { SECTIONS } from "./sections";

/** The landing page's sticky bar: the section anchors and the way in. */
export function SiteHeader() {
  return (
    <header className="sticky top-0 z-10 border-b border-border/60 bg-background/80 backdrop-blur">
      <div className="mx-auto flex w-full max-w-shell items-center gap-6 px-6 py-3.5">
        <BrandMark />
        <nav
          aria-label="Sections"
          className="hidden items-center gap-6 md:flex"
        >
          {SECTIONS.map((section) => (
            <Link
              key={section.id}
              href={`#${section.id}`}
              className="text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              {section.label}
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-1.5">
          <Button asChild variant="ghost" size="sm">
            <Link href="/login">Sign in</Link>
          </Button>
          <Button asChild size="sm">
            <Link href="/console">Open the console</Link>
          </Button>
        </div>
      </div>
    </header>
  );
}
