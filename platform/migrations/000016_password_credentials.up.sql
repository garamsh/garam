-- The verifier that authenticates a password, and the bound on how
-- often one may be computed (ADR-0041).
--
-- A verifier is garam's own authentication material rather than a value
-- held on someone else's behalf, so ADR-0036 — which keeps secret
-- values out of garam entirely — neither forbids this table nor is
-- weakened by it. What is stored is a one-way function of the password
-- under Argon2id, never the password.
--
-- One row per user that has a password, keyed by the user it
-- authenticates: `users` is read on every authenticated request, and a
-- column there would carry the verifier onto that path. Most users have
-- none, and withdrawing password sign-in deletes the row rather than
-- nulling a column. updated_at is when the verifier was last written,
-- which a re-hash at raised parameters moves — the one signal that says
-- whether the population has converged onto the current parameters.
CREATE TABLE password_credentials (
    user_grn   text PRIMARY KEY REFERENCES users (grn) ON DELETE CASCADE,
    verifier   text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- One row per password verification in flight. Argon2id costs 19 MiB
-- and a deliberately expensive computation, and the unknown-username
-- path spends it too, so an unauthenticated caller driving one per
-- request holds a better lever than the row-per-request ADR-0030
-- bounds. The attempt is recorded before any verifier is computed and
-- the count is decided in the statement that writes, so the bound holds
-- across every replica rather than per process.
--
-- The key is an opaque identifier the handler generates and consumes,
-- not a secret anyone presents, so — unlike a session token or a login
-- state — there is nothing here to hash.
CREATE TABLE password_attempts (
    attempt_id text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

-- Recording an attempt deletes the attempts that have lapsed, so the
-- table holds the verifications in flight rather than every one ever
-- made. This index is what that sweep reads.
CREATE INDEX password_attempts_expires_at_idx ON password_attempts (expires_at);
