/**
 * The auth feature's surface for a Server Component: who is signed in
 * and their sessions; nothing else under `features/auth/` is public.
 * The ways in are the site's (ADR-0131). The way out is a Server
 * Action, declared in `actions.ts` — reaching this file from a client
 * module would pull the server-only backend client into the browser
 * bundle.
 */
export { getSession, type User } from "./api/get-session";
export {
  listMySessions,
  type Session,
  type SessionsRead,
} from "./api/sessions";
