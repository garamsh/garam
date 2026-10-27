import type { Metadata } from "next";

import "@/styles/globals.css";

export const metadata: Metadata = {
  title: "cowork",
  description: "Pages people and agents edit together",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-white text-neutral-900 antialiased">
        {children}
      </body>
    </html>
  );
}
