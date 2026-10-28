/**
 * The auth feature's surface for a Server Component: who is signed in
 * and the ways in; nothing else under `features/auth/` is public. The
 * Server Actions are declared in `actions.ts` — reaching this file from
 * a client module would pull the server-only backend client into the
 * browser bundle.
 */
export { getSession, type User } from "./api/get-session";
export { listProviders } from "./api/list-providers";
export { GoogleSignInButton } from "./components/google-sign-in-button";
export { PasswordRegisterForm } from "./components/password-register-form";
export { PasswordSignInForm } from "./components/password-sign-in-form";
