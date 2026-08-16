import type { Metadata } from "next";
import { Geist, Geist_Mono, Poppins } from "next/font/google";
import "./globals.css";

// `--font-sans`, `--font-geist-mono` and `--font-poppins` are the
// variables the theme block in globals.css reads.
const geistSans = Geist({
  variable: "--font-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

// The brand face, per the logo kit: SemiBold is the only weight the
// wordmark uses.
const poppins = Poppins({
  variable: "--font-poppins",
  subsets: ["latin"],
  weight: ["600"],
});

/** Head metadata for every route that does not set its own. */
export const metadata: Metadata = {
  title: "garam",
  description: "Identity, organization, and authorization for shared work.",
};

/** The document itself: the root layout owns `<html>` and `<body>`. */
export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} ${poppins.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}
