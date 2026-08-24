import Link from "next/link";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { Card, CardFooter, CardHeader } from "@/components/ui/card";

/**
 * What a sign-in shell route shows in place of its form when the API
 * did not answer. Every way in from here runs through it, so offering
 * the form would be offering a door that is shut.
 */
export function ApiUnreachableCard({ subject }: { subject: string }) {
  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          {API_UNREACHABLE_TITLE}
        </h1>
        <p className="text-sm text-pretty text-muted-foreground">
          <ApiUnreachableReason subject={subject} />
        </p>
      </CardHeader>
      <CardFooter className="justify-center text-xs text-muted-foreground">
        <Link href="/" className="transition-colors hover:text-foreground">
          Back to the home page
        </Link>
      </CardFooter>
    </Card>
  );
}
