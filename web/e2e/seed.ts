import { createHash, randomBytes } from "node:crypto";

import { Client } from "pg";

/** The rows `seedSignedInOrg` wrote, and the cookie that reaches them. */
export type Seed = {
  sessionToken: string;
  orgSlug: string;
  orgName: string;
  userGrn: string;
  nickname: string;
  groupName: string;
  projectName: string;
};

/** A signed-in user without an organization: the invitee side of a flow. */
export type SeededUser = {
  sessionToken: string;
  userGrn: string;
  nickname: string;
};

const DATABASE_URL =
  process.env.GARAM_E2E_DATABASE_URL ??
  "postgres://garam:garam@localhost:55432/garam?sslmode=disable";

/** Mirrors the backend's `authn.HashToken`: hex-encoded SHA-256. */
function hashToken(token: string): string {
  return createHash("sha256").update(token).digest("hex");
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
 * Writes the rows a completed OAuth callback would have written, since
 * the round trip needs Google and cannot run unattended. Everything
 * downstream of the returned cookie is the real backend.
 */
export async function seedSignedInOrg(): Promise<Seed> {
  const suffix = randomBytes(6).toString("hex");
  const seed: Seed = {
    sessionToken: randomBytes(32).toString("base64url"),
    orgSlug: `e2e-${suffix}`,
    orgName: `E2E Org ${suffix}`,
    userGrn: `grn:root:default:user:e2e-${suffix}`,
    nickname: `e2e-user-${suffix}`,
    groupName: "Engineering",
    projectName: "Frontend",
  };
  const orgGrn = `grn:root:default:org:${seed.orgSlug}`;

  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
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
    await client.query(
      `INSERT INTO groups (grn, org_grn, name, created_at, updated_at)
       VALUES ($1, $2, $3, now(), now())`,
      [
        `grn:${seed.orgSlug}:default:group:engineering`,
        orgGrn,
        seed.groupName,
      ],
    );
    await client.query(
      `INSERT INTO projects (grn, org_grn, name, created_at, updated_at)
       VALUES ($1, $2, $3, now(), now())`,
      [`grn:${seed.orgSlug}:default:project:frontend`, orgGrn, seed.projectName],
    );
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
  return seed;
}

/**
 * Seeds a signed-in user who belongs to no organization, for flows that
 * hand a second identity something to accept.
 */
export async function seedSignedInUser(): Promise<SeededUser> {
  const suffix = randomBytes(6).toString("hex");
  const user: SeededUser = {
    sessionToken: randomBytes(32).toString("base64url"),
    userGrn: `grn:root:default:user:e2e-${suffix}`,
    nickname: `e2e-user-${suffix}`,
  };

  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
    await insertUserAndSession(client, user);
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
  return user;
}
