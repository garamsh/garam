import type { Metadata } from "next";
import { Geist_Mono } from "next/font/google";
// Pretendard is the one UI face. Its dynamic-subset build binds each
// of its files to a unicode-range, which next/font/local cannot
// express, so the package's own stylesheet is imported and the bundler
// self-hosts the files it names: a page loads only the subsets its
// text needs.
import "pretendard/dist/web/variable/pretendardvariable-dynamic-subset.css";

import { THEME_SCRIPT } from "@/lib/theme";
import "@/styles/globals.css";

// `--font-geist-mono` is the variable the theme block in globals.css
// reads for `font-mono`.
const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

/** Head metadata for every route that does not set its own. */
export const metadata: Metadata = {
  title: "garam",
  description: "Identity, organization, and authorization for shared work.",
};

/**
 * The document itself: the root layout owns `<html>` and `<body>`. The
 * theme script sets a class on `<html>` before React hydrates it, which
 * is the one attribute difference `suppressHydrationWarning` admits.
 */
export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${geistMono.variable} h-full antialiased`}
      suppressHydrationWarning
    >
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
      </head>
      <body className="flex min-h-full flex-col">{children}</body>
    </html>
  );
}
