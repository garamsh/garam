import Link from "next/link";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@garam/design/components/api-unreachable";
import { Card, CardFooter, CardHeader } from "@garam/design/ui/card";

/**
 * What a sign-in shell route shows in place of its form when the API
 * did not answer. Every way in from here runs through it, so offering
 * the form would be offering a door that is shut.
 */
export function ApiUnreachableCard({ subject }: { subject: string }) {
  return (
    <Card className="w-full max-w-sm [--card-spacing:--spacing(6)]">
      <CardHeader className="gap-2">
        <h1 className="text-xl tracking-tight">{API_UNREACHABLE_TITLE}</h1>
        <p className="text-pretty text-muted-foreground">
          <ApiUnreachableReason subject={subject} />
        </p>
      </CardHeader>
      <CardFooter className="justify-center text-muted-foreground">
        <Link href="/" className="transition-colors hover:text-foreground">
          Back to the home page
        </Link>
      </CardFooter>
    </Card>
  );
}
