import Link from "next/link";

import { BrandMark } from "@/components/brand-mark";

import { SECTIONS } from "./sections";

const PRODUCT_LINKS = [
  { href: "/console", label: "Open the console" },
  { href: "/login", label: "Sign in" },
];

/** Closes the landing page: the brand, the way in, and the anchors again. */
export function SiteFooter() {
  return (
    <footer className="border-t border-border/60">
      <div className="mx-auto grid w-full max-w-shell gap-10 px-6 py-14 md:grid-cols-[2fr_1fr_1fr]">
        <div className="flex flex-col items-start gap-3">
          <BrandMark />
          <p className="max-w-xs text-sm text-pretty text-muted-foreground">
            Identity, organization, and authorization for shared work.
          </p>
        </div>
        <FooterColumn title="Product" links={PRODUCT_LINKS} />
        <FooterColumn
          title="On this page"
          links={SECTIONS.map((section) => ({
            href: `#${section.id}`,
            label: section.label,
          }))}
        />
      </div>
      <div className="border-t border-border/60">
        <div className="mx-auto flex w-full max-w-shell flex-wrap items-center justify-between gap-2 px-6 py-6 text-xs text-muted-foreground">
          <span>garam</span>
          <span className="font-mono">
            grn:&lt;organization&gt;:&lt;project&gt;:&lt;service_type&gt;:&lt;identifier&gt;
          </span>
        </div>
      </div>
    </footer>
  );
}

function FooterColumn({
  title,
  links,
}: {
  title: string;
  links: { href: string; label: string }[];
}) {
  return (
    <div className="flex flex-col gap-3">
      <h2 className="text-eyebrow font-medium text-muted-foreground uppercase">
        {title}
      </h2>
      <ul className="flex flex-col gap-2.5">
        {links.map((link) => (
          <li key={link.href}>
            <Link
              href={link.href}
              className="text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              {link.label}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
