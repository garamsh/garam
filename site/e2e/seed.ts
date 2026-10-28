import { createHash, randomBytes } from "node:crypto";

import { Client } from "pg";

/** A signed-in user without an organization: the invitee side of a flow. */
export type SeededUser = {
  sessionToken: string;
  userGrn: string;
  userSlug: string;
  nickname: string;
};

/** The rows `seedSignedInOrg` wrote: its owner, and the cookie that reaches them. */
export type Seed = SeededUser & {
  orgSlug: string;
  orgName: string;
};

/**
 * The database the suite seeds into. `make -C site e2e` supplies it
 * from the port compose published; unset, there is nothing to guess at.
 */
function databaseUrl(): string {
  const url = process.env.GARAM_E2E_DATABASE_URL;
  if (!url) {
    throw new Error(
      "GARAM_E2E_DATABASE_URL is unset — run the suite through `make -C site e2e`",
    );
  }
  return url;
}

const DATABASE_URL = databaseUrl();

/** Mirrors the backend's `authn.HashToken`: hex-encoded SHA-256. */
function hashToken(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}

function newUser(): SeededUser {
  const suffix = randomBytes(6).toString("hex");
  return {
    sessionToken: randomBytes(32).toString("base64url"),
    userGrn: `grn:root:default:user:e2e-${suffix}`,
    userSlug: `e2e-${suffix}`,
    nickname: `e2e-user-${suffix}`,
  };
}

/** Runs `write` in one transaction on the suite's database. */
async function transaction(write: (client: Client) => Promise<void>) {
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
    await write(client);
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
}

/** Writes the user and session rows a completed OAuth callback wrote. */
async function insertUserAndSession(
  client: Client,
  user: SeededUser,
): Promise<void> {
  await client.query(
    `INSERT INTO users (grn, nickname, created_at, updated_at)
     VALUES ($1, $2, now(), now())`,
    [user.userGrn, user.nickname],
  );
  await client.query(
    `INSERT INTO sessions (token_hash, user_grn, created_at, expires_at)
     VALUES ($1, $2, now(), now() + interval '1 hour')`,
    [hashToken(user.sessionToken), user.userGrn],
  );
}

/**
 * Writes the rows a completed OAuth callback and a created organization
 * would have written, since the round trip needs Google and cannot run
 * unattended: a user who owns an organization, and that user's session.
 * Everything downstream of the returned cookie is the real backend.
 */
export async function seedSignedInOrg(): Promise<Seed> {
  const user = newUser();
  const suffix = user.userSlug.slice("e2e-".length);
  const seed: Seed = {
    ...user,
    orgSlug: `e2e-${suffix}`,
    orgName: `E2E Org ${suffix}`,
  };
  const orgGrn = `grn:root:default:org:${seed.orgSlug}`;
  await transaction(async (client) => {
    await insertUserAndSession(client, seed);
    await client.query(
      `INSERT INTO orgs (grn, name, created_at, updated_at)
       VALUES ($1, $2, now(), now())`,
      [orgGrn, seed.orgName],
    );
    await client.query(
      `INSERT INTO org_members (org_grn, user_grn, role, created_at)
       VALUES ($1, $2, 'owner', now())`,
      [orgGrn, seed.userGrn],
    );
  });
  return seed;
}

/**
 * Seeds a signed-in user who belongs to no organization, for flows that
 * hand a second identity something to accept.
 */
export async function seedSignedInUser(): Promise<SeededUser> {
  const user = newUser();
  await transaction((client) => insertUserAndSession(client, user));
  return user;
}
