import { createHash, randomBytes } from "node:crypto";

import { Client } from "pg";

/** The rows `seedSignedInOrg` wrote, and the cookie that reaches them. */
export type Seed = {
  sessionToken: string;
  orgSlug: string;
  orgName: string;
  userGrn: string;
  /** The GRN's last segment: what the console shows in place of the GRN. */
  userSlug: string;
  nickname: string;
  groupName: string;
  projectName: string;
  /** The operator the org has registered, which a definition may name. */
  operatorIdentifier: string;
  /** The agent the org holds, and the states its two parties reported. */
  agentIdentifier: string;
  agentProvisioningState: string;
  agentRuntimeState: string;
  /**
   * The organization that has granted its half of a consent pair with
   * the seeded one, which has not granted its own: the state a relation
   * waits in until the second half lands (ADR-0035).
   */
  partnerOrgSlug: string;
  partnerOrgName: string;
};

/** A signed-in user without an organization: the invitee side of a flow. */
export type SeededUser = {
  sessionToken: string;
  userGrn: string;
  userSlug: string;
  nickname: string;
};

/**
 * The database the suite seeds into. `make web-e2e` supplies it from the
 * port compose published; unset, there is nothing to guess at.
 */
function databaseUrl(): string {
  const url = process.env.GARAM_E2E_DATABASE_URL;
  if (!url) {
    throw new Error(
      "GARAM_E2E_DATABASE_URL is unset — run the suite through `make web-e2e`",
    );
  }
  return url;
}

const DATABASE_URL = databaseUrl();

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
    userSlug: `e2e-${suffix}`,
    nickname: `e2e-user-${suffix}`,
    groupName: "Engineering",
    projectName: "Frontend",
    operatorIdentifier: "field-one",
    agentIdentifier: randomBytes(8).toString("hex"),
    agentProvisioningState: "failed",
    agentRuntimeState: "disconnected",
    partnerOrgSlug: `e2e-partner-${suffix}`,
    partnerOrgName: `E2E Partner ${suffix}`,
  };
  const orgGrn = `grn:root:default:org:${seed.orgSlug}`;
  const partnerGrn = `grn:root:default:org:${seed.partnerOrgSlug}`;

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
      [`grn:${seed.orgSlug}:default:group:engineering`, orgGrn, seed.groupName],
    );
    await client.query(
      `INSERT INTO projects (grn, org_grn, name, created_at, updated_at)
       VALUES ($1, $2, $3, now(), now())`,
      [
        `grn:${seed.orgSlug}:default:project:frontend`,
        orgGrn,
        seed.projectName,
      ],
    );
    // An agent definition may only name an operator the organization
    // has registered (ADR-0045), so the record comes with the org.
    await client.query(
      `INSERT INTO operators (grn, org_grn, registered_by, created_at)
       VALUES ($1, $2, $3, now())`,
      [
        `grn:${seed.orgSlug}:default:operator:${seed.operatorIdentifier}`,
        orgGrn,
        seed.userGrn,
      ],
    );
    // garam learns an agent exists from an operator on the machine
    // listener, and both states are reported there too (ADR-0053) —
    // neither route a browser can reach — so the row is seeded.
    await client.query(
      `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
       VALUES ($1, $2, $3, $4, now(), now())`,
      [
        `grn:${seed.orgSlug}:default:agent:${seed.agentIdentifier}`,
        orgGrn,
        seed.agentProvisioningState,
        seed.agentRuntimeState,
      ],
    );
    // The partner organization the seeded one may consent with, and
    // the half that organization has already granted. Consent is
    // granted per organization by an admin of it, and the seeded user
    // is an admin of neither side but its own, so the far half is
    // seeded rather than granted through the console.
    await client.query(
      `INSERT INTO orgs (grn, name, created_at, updated_at)
       VALUES ($1, $2, now(), now())`,
      [partnerGrn, seed.partnerOrgName],
    );
    // The pair is stored ordered and which half a grant moves follows
    // from where the granter sorts, which is the backend's own rule —
    // so the row is written the way `GrantConsent` writes it.
    await client.query(
      `INSERT INTO message_consents (org_a, org_b, a_consented_at, b_consented_at)
       VALUES (least($1, $2), greatest($1, $2),
               CASE WHEN $2 < $1 THEN now() END,
               CASE WHEN $2 > $1 THEN now() END)`,
      [orgGrn, partnerGrn],
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
    userSlug: `e2e-${suffix}`,
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
