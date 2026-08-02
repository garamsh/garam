import Link from "next/link";

import { Button } from "@/components/ui/button";

export default function NotFound() {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-4 p-6">
      <h1 className="font-heading text-2xl font-semibold">Not found</h1>
      <p className="text-sm text-muted-foreground">
        That page does not exist, or it is not yours to see.
      </p>
      <Button asChild variant="outline">
        <Link href="/orgs">Back to organizations</Link>
      </Button>
    </main>
  );
}
