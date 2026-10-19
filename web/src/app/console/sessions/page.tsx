import type { Metadata } from "next";

import { listMySessions } from "@/features/auth";

import { ApiUnreachable } from "../_components/api-unreachable";
import { SESSIONS_LABEL } from "../_components/navigation";
import { ListPage } from "../_components/templates/list-page";
import { SessionTable } from "./_components/session-table";

/** Head metadata for the sessions route; Next.js reads it by name. */
export const metadata: Metadata = { title: `${SESSIONS_LABEL} — garam` };

/**
 * The signed-in person's sessions, browser and CLI (ADR-0120), newest
 * first, the one this page was opened on marked. Each is revoked from
 * its row; revoking this one signs this browser out.
 */
export default async function SessionsPage() {
  const read = await listMySessions();
  if (read.status === "unreachable") {
    return (
      <ListPage title={SESSIONS_LABEL}>
        <ApiUnreachable subject="your sessions" />
      </ListPage>
    );
  }
  return (
    <ListPage title={SESSIONS_LABEL} count={read.data.length}>
      <SessionTable sessions={read.data} />
    </ListPage>
  );
}
