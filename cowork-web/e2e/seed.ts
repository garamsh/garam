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
      "GARAM_E2E_DATABASE_URL is unset — run the suite through `make -C cowork-web suites`",
    );
  return url;
}

/** Mirrors the platform's `authn.HashToken`: hex-encoded SHA-256. */
const hashToken = (token: string) =>
  createHash("sha256").update(token).digest("hex");

async function insertPerson(client: Client, nickname: string): Promise<Person> {
  const person = {
    nickname,
    userGrn: `grn:root/iam/user/${nickname}-${randomUUID().slice(0, 8)}`,
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
 * The platform marks a write for the policy service only while its feed
 * is on, so with it off nothing would ever be delivered and the wait
 * below would end at once on an outbox that never held a mark.
 */
async function requireFeedOn(client: Client): Promise<void> {
  const { rows } = await client.query("SELECT enabled FROM policy_feed");
  if (rows[0]?.enabled !== true)
    throw new Error(
      "the platform's policy feed is off, so nothing the seed writes reaches the policy service — run `make -C cowork-web stack-up`",
    );
}

/**
 * Waits until the policy service holds what the seed wrote. A write
 * marks its entities in the platform's outbox in its own transaction,
 * and a delivery pass deletes a mark in the transaction that commits
 * after the service accepted the entity, so no mark left for this
 * organization or these people is the service holding them. A mark the
 * service refused for good is set aside and never goes away: that
 * fails here, naming it, and not as a denial in a spec.
 */
async function policyHolds(
  client: Client,
  orgGrn: string,
  people: Person[],
): Promise<void> {
  const grns = [orgGrn, ...people.map((p) => p.userGrn)];
  // A role link's id is the organization's GRN, a space and the
  // member's GRN.
  const marks = `SELECT entity_type, entity_id, set_aside_at IS NOT NULL AS set_aside
     FROM policy_outbox WHERE entity_id = ANY($1) OR starts_with(entity_id, $2)`;
  const deadline = Date.now() + 30_000;
  for (;;) {
    const { rows } = await client.query(marks, [grns, `${orgGrn} `]);
    const refused = rows.filter((r) => r.set_aside);
    if (refused.length > 0)
      throw new Error(
        `the policy service refused ${refused.map((r) => `${r.entity_type} ${r.entity_id}`).join(", ")}; the platform's outbox has set it aside`,
      );
    if (rows.length === 0) return;
    if (Date.now() > deadline)
      throw new Error(
        `the policy service did not receive ${rows.length} of the seed's entities within 30s (${orgGrn}) — is \`policy-feed\` running and the api delivering?`,
      );
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

/**
 * Writes what the platform holds for an organization with the named
 * members, each signed in, and one person signed in who belongs to
 * none: the rows a sign-in and an invitation would have written. It
 * returns once the policy service holds them, since that service, and
 * not these tables, is what cowork asks (see `policyHolds`).
 */
export async function seedWorkspace(
  ...nicknames: string[]
): Promise<Workspace> {
  const client = new Client({ connectionString: databaseUrl() });
  await client.connect();
  try {
    await client.query("BEGIN");
    await requireFeedOn(client);
    const orgSlug = `cw-${randomUUID().slice(0, 8)}`;
    const orgGrn = `grn:root/iam/org/${orgSlug}`;
    await client.query(
      `INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ($1, $2, now(), now())`,
      [orgGrn, orgSlug],
    );
    // The identity cowork holds in the organization, which the machine
    // listener admits its token request for: CreateOrg writes it with
    // the organization.
    await client.query(
      `INSERT INTO service_principals (grn, org_grn, created_at)
       VALUES ('grn:' || grn_id($1::text) || '/iam/service/cowork', $1, now())`,
      [orgGrn],
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
    await policyHolds(client, orgGrn, [...members, outsider]);
    return { orgSlug, members, outsider };
  } catch (err) {
    await client.query("ROLLBACK");
    throw err;
  } finally {
    await client.end();
  }
}
