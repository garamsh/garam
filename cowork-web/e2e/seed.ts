import { createHash, randomBytes, randomUUID } from "node:crypto";

import { Client } from "pg";

/** A person signed in to the platform, and the cookie that reaches them. */
export type Person = {
  nickname: string;
  userGrn: string;
  sessionToken: string;
};

/** An organization the suite created, with its members signed in. */
export type Workspace = {
  orgSlug: string;
  members: Person[];
  outsider: Person;
};

function databaseUrl(): string {
  const url = process.env.GARAM_E2E_DATABASE_URL;
  if (!url)
    throw new Error(
      "GARAM_E2E_DATABASE_URL is unset — run the suite through `make cowork-web-e2e`",
    );
  return url;
}

/** Mirrors the platform's `authn.HashToken`: hex-encoded SHA-256. */
const hashToken = (token: string) =>
  createHash("sha256").update(token).digest("hex");

async function insertPerson(client: Client, nickname: string): Promise<Person> {
  const person = {
    nickname,
    userGrn: `grn:root:default:user:${nickname}-${randomUUID().slice(0, 8)}`,
    sessionToken: randomBytes(32).toString("hex"),
  };
  await client.query(
    `INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ($1, $2, now(), now())`,
    [person.userGrn, person.nickname],
  );
  await client.query(
    `INSERT INTO sessions (token_hash, user_grn, created_at, expires_at)
     VALUES ($1, $2, now(), now() + interval '1 hour')`,
    [hashToken(person.sessionToken), person.userGrn],
  );
  return person;
}

/**
 * Writes what the platform holds for an organization with the named
 * members, each signed in, and one person signed in who belongs to
 * none: the rows a sign-in and an invitation would have written.
 */
export async function seedWorkspace(
  ...nicknames: string[]
): Promise<Workspace> {
  const client = new Client({ connectionString: databaseUrl() });
  await client.connect();
  try {
    await client.query("BEGIN");
    const orgSlug = `cw-${randomUUID().slice(0, 8)}`;
    const orgGrn = `grn:root:default:org:${orgSlug}`;
    await client.query(
      `INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ($1, $2, now(), now())`,
      [orgGrn, orgSlug],
    );
    const members: Person[] = [];
    for (const [i, nickname] of nicknames.entries()) {
      const person = await insertPerson(client, nickname);
      await client.query(
        `INSERT INTO org_members (org_grn, user_grn, role, created_at) VALUES ($1, $2, $3, now())`,
        [orgGrn, person.userGrn, i === 0 ? "owner" : "member"],
      );
      members.push(person);
    }
    const outsider = await insertPerson(client, "outsider");
    await client.query("COMMIT");
    return { orgSlug, members, outsider };
  } catch (err) {
    await client.query("ROLLBACK");
    throw err;
  } finally {
    await client.end();
  }
}
