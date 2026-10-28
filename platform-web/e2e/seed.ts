import { createHash, randomBytes, randomUUID } from "node:crypto";

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
  /** The operator the org has registered. */
  operatorIdentifier: string;
  /** What the org calls that operator (ADR-0074). */
  operatorName: string;
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
    operatorName: "Field one",
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
    // The operator the organization has registered comes with the org.
    await client.query(
      `INSERT INTO operators (grn, org_grn, registered_by, created_at, name)
       VALUES ($1, $2, $3, now(), $4)`,
      [
        `grn:${seed.orgSlug}:default:operator:${seed.operatorIdentifier}`,
        orgGrn,
        seed.userGrn,
        seed.operatorName,
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

/**
 * Writes a session for `userGrn` that expired an hour ago and answers
 * its cookie value: what a browser still holds after the backend has
 * stopped honouring it, which the proxy lets through on sight.
 */
export async function seedExpiredSession(userGrn: string): Promise<string> {
  const token = randomBytes(32).toString("base64url");
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query(
      `INSERT INTO sessions (token_hash, user_grn, created_at, expires_at)
       VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')`,
      [hashToken(token), userGrn],
    );
  } finally {
    await client.end();
  }
  return token;
}

/** The hosted operator `seedHostedOperator` wrote, and its grant. */
export type HostedSeed = {
  identifier: string;
  grn: string;
  name: string;
  endpoint: string;
};

/**
 * Registers a hosted operator with a control endpoint and a grant
 * carrying `operations` for the seeded operator as its controller, and
 * assigns the seeded agent to that controller. An administrator does
 * each of these through the console, which `control-service.spec.ts`
 * drives once; the other tests start from the state it leaves.
 */
export async function seedHostedOperator(
  seed: Seed,
  endpoint: string | null,
  operations: readonly string[],
  // Who the grant names as having granted it. Anything but a GRN is a
  // row garam cannot read back, which is how a test makes the
  // delegation read fail.
  grantedBy: string = seed.userGrn,
): Promise<HostedSeed> {
  const hosted: HostedSeed = {
    identifier: "hosted",
    grn: `grn:${seed.orgSlug}:default:operator:hosted`,
    name: "Hosted control",
    endpoint: endpoint ?? "",
  };
  const orgGrn = `grn:root:default:org:${seed.orgSlug}`;
  const controller = `grn:${seed.orgSlug}:default:operator:${seed.operatorIdentifier}`;
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
    await client.query(
      `INSERT INTO operators (grn, org_grn, registered_by, created_at, name, control_endpoint)
       VALUES ($1, $2, $3, now(), $4, $5)`,
      [hosted.grn, orgGrn, seed.userGrn, hosted.name, endpoint],
    );
    if (operations.length > 0) {
      const grantId = randomBytes(16).toString("hex");
      await client.query(
        `INSERT INTO operator_delegations
           (id, org_grn, operator_grn, request_id, operations, expires_at, granted_by, granted_at)
         VALUES ($1, $2, $3, $4, $5, now() + interval '1 day', $6, now())`,
        [grantId, orgGrn, hosted.grn, `seed-${grantId}`, operations, grantedBy],
      );
      await client.query(
        `INSERT INTO operator_delegation_controllers (delegation_id, controller_grn)
         VALUES ($1, $2)`,
        [grantId, controller],
      );
    }
    await client.query(
      `INSERT INTO agent_assignments (agent_grn, assignee_grn, epoch, created_at, updated_at)
       VALUES ($1, $2, 1, now(), now())`,
      [`grn:${seed.orgSlug}:default:agent:${seed.agentIdentifier}`, controller],
    );
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
  return hosted;
}

/** One operation authority garam recorded, read by its token. */
export type RecordedAuthority = {
  operation: string;
  actorGrn: string;
  audienceGrn: string;
  targetGrn: string;
  requestId: string;
  bodySha256: string;
  requestTarget: string | null;
};

/**
 * Reads back what garam bound to the authority a control request
 * carried, by the hash garam stores of it: whatever the console sent is
 * checked against garam's own record and not against the console.
 */
export async function recordedAuthority(
  authority: string,
): Promise<RecordedAuthority | null> {
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    const { rows } = await client.query<RecordedAuthority>(
      `SELECT operation, actor_grn AS "actorGrn", audience_grn AS "audienceGrn",
              target_grn AS "targetGrn", request_id AS "requestId",
              body_sha256 AS "bodySha256", request_target AS "requestTarget"
       FROM operation_authorities WHERE token_hash = $1`,
      [hashToken(authority)],
    );
    return rows[0] ?? null;
  } finally {
    await client.end();
  }
}

/**
 * Adds a member of the seeded organization holding the member role and
 * nothing else, and answers that member's session token.
 */
export async function seedMember(seed: Seed): Promise<SeededUser> {
  const suffix = randomBytes(6).toString("hex");
  const member: SeededUser = {
    sessionToken: randomBytes(32).toString("base64url"),
    userGrn: `grn:root:default:user:e2e-${suffix}`,
    userSlug: `e2e-${suffix}`,
    nickname: `e2e-member-${suffix}`,
  };
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
    await insertUserAndSession(client, member);
    await client.query(
      `INSERT INTO org_members (org_grn, user_grn, role, created_at)
       VALUES ($1, $2, 'member', now())`,
      [`grn:root:default:org:${seed.orgSlug}`, member.userGrn],
    );
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
  return member;
}

/**
 * Adds a member of the seeded organization whom an active policy denies
 * `operator:read`, and answers that member's session token. Policies
 * are seeded rather than written through the console because the
 * subject is a second identity; the change notification the policy
 * table raises reaches the engine as a console write would.
 */
export async function seedMemberDeniedOperatorRead(
  seed: Seed,
): Promise<SeededUser> {
  const suffix = randomBytes(6).toString("hex");
  const member: SeededUser = {
    sessionToken: randomBytes(32).toString("base64url"),
    userGrn: `grn:root:default:user:e2e-${suffix}`,
    userSlug: `e2e-${suffix}`,
    nickname: `e2e-member-${suffix}`,
  };
  const orgGrn = `grn:root:default:org:${seed.orgSlug}`;
  const policyGrn = `grn:${seed.orgSlug}:default:policy:deny-operator-read`;
  const document = {
    Version: "2026-07-25",
    Statement: [
      {
        Sid: "DenyOperatorRead",
        Effect: "Deny",
        Action: ["operator:read"],
        Resource: [orgGrn, `grn:${seed.orgSlug}:*`],
      },
    ],
  };
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
    await insertUserAndSession(client, member);
    await client.query(
      `INSERT INTO org_members (org_grn, user_grn, role, created_at)
       VALUES ($1, $2, 'member', now())`,
      [orgGrn, member.userGrn],
    );
    await client.query(
      `INSERT INTO policies (grn, org_grn, name, status, default_version, created_at, updated_at)
       VALUES ($1, $2, 'deny-operator-read', 'active', 1, now(), now())`,
      [policyGrn, orgGrn],
    );
    await client.query(
      `INSERT INTO policy_versions (policy_grn, version, document, created_by, created_at)
       VALUES ($1, 1, $2, $3, now())`,
      [policyGrn, JSON.stringify(document), seed.userGrn],
    );
    await client.query(
      `INSERT INTO policy_attachments (policy_grn, subject_grn, created_at)
       VALUES ($1, $2, now())`,
      [policyGrn, member.userGrn],
    );
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
  return member;
}

/**
 * Records that the operator's newest key obtained a token. The local
 * stack runs no machine listener, so nothing exchanges an issued key
 * there; this writes what an exchange leaves behind, which is all the
 * console reads of it (ADR-0102).
 */
export async function seedKeyExchange(
  seed: Seed,
  operatorIdentifier: string = seed.operatorIdentifier,
): Promise<void> {
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    const result = await client.query(
      `UPDATE operator_keys SET last_exchanged_at = now()
        WHERE id = (SELECT max(id) FROM operator_keys WHERE operator_grn = $1)`,
      [`grn:${seed.orgSlug}:default:operator:${operatorIdentifier}`],
    );
    if (result.rowCount !== 1) {
      throw new Error(
        `no key of ${operatorIdentifier} to record an exchange for`,
      );
    }
  } finally {
    await client.end();
  }
}

/** The hosts `seedHosts` wrote, by the name each was approved under. */
export type SeededHosts = { pending: string; active: string };

/**
 * Writes two approvals by the seeded owner: one the host has not spent,
 * and one spent under standalone-enrollment.v2, whose supervisor runs a
 * new agent. Only a host on the machine listener spends an approval
 * (ADR-0082), and no route a browser reaches does, so the spending is
 * seeded as that route writes it.
 */
export async function seedHosts(seed: Seed): Promise<SeededHosts> {
  const suffix = randomBytes(4).toString("hex");
  const hosts: SeededHosts = {
    pending: `pending-${suffix}`,
    active: `active-${suffix}`,
  };
  const orgGrn = `grn:root:default:org:${seed.orgSlug}`;
  const grnOf = (type: string) =>
    `grn:${seed.orgSlug}:default:${type}:${randomBytes(8).toString("hex")}`;
  const approve = (client: Client, host: string, agentGrn: string) =>
    client.query<{ id: string; supervisor_grn: string }>(
      `INSERT INTO agent_enrollments
         (id, org_grn, agent_grn, supervisor_grn, host_name, request_id,
          approved_by, secret_sha256, expires_at, created_at)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now() + interval '1 hour', now())
       RETURNING id, supervisor_grn`,
      [
        randomUUID(),
        orgGrn,
        agentGrn,
        grnOf("supervisor"),
        host,
        `e2e-${randomBytes(6).toString("hex")}`,
        seed.userGrn,
        randomBytes(32),
      ],
    );
  const client = new Client({ connectionString: DATABASE_URL });
  await client.connect();
  try {
    await client.query("BEGIN");
    await approve(client, hosts.pending, grnOf("agent"));

    const agentGrn = grnOf("agent");
    await client.query(
      `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
       VALUES ($1, $2, 'ready', 'connected', now(), now())`,
      [agentGrn, orgGrn],
    );
    await client.query(
      `INSERT INTO agent_executions (agent_grn, updated_at) VALUES ($1, now())`,
      [agentGrn],
    );
    const {
      rows: [spent],
    } = await approve(client, hosts.active, agentGrn);
    await client.query(
      `UPDATE agent_enrollments
          SET consumed_at = now(), request_sha256 = $2, epoch = 1, lineage = $3
        WHERE id = $1`,
      [spent!.id, randomBytes(32), randomUUID()],
    );
    await client.query(
      `INSERT INTO agent_supervisors (grn, agent_grn, enrollment_id, host_name, registered_at)
       VALUES ($1, $2, $3, $4, now())`,
      [spent!.supervisor_grn, agentGrn, spent!.id, hosts.active],
    );
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    await client.end();
  }
  return hosts;
}
