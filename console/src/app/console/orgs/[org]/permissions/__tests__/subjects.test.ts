import { describe, expect, it } from "vitest";

import {
  describeSubject,
  inspectSubject,
  listSubjects,
  reachOf,
  resolveSubjectQuery,
  type Catalogs,
  type PolicyScope,
  type Subject,
} from "@/app/console/orgs/[org]/permissions/_components/subjects";
import type { Group, Policy } from "@/features/orgs";
import {
  aliceMembership,
  baselineRead,
  denyDeletes,
  engineering,
  reportedAgent,
} from "@/testing/mocks/handlers";

const catalogs: Catalogs = {
  teams: { status: "ok", data: [engineering] },
  people: { status: "ok", data: [aliceMembership] },
  agents: { status: "ok", data: [reportedAgent] },
};

const alice: Subject = {
  grn: aliceMembership.userGrn,
  kind: "person",
  identifier: "alice",
  label: "Alice",
};

const draft: Policy = {
  ...baselineRead,
  grn: "grn:acme:default:policy:dddd",
  name: "Draft",
  status: "draft",
};
const direct: Policy = {
  ...denyDeletes,
  grn: "grn:acme:default:policy:eeee",
  name: "Direct",
};

function attached(policy: Policy, ...subjects: string[]): PolicyScope {
  return {
    policy,
    attachments: {
      status: "ok",
      data: subjects.map((subjectGrn) => ({
        policyGrn: policy.grn,
        subjectGrn,
        createdAt: "2026-01-02T03:04:05Z",
      })),
    },
  };
}

function membersOf(team: Group, ...grns: string[]) {
  return {
    team,
    members: {
      status: "ok" as const,
      data: grns.map((memberGrn) => ({ memberGrn })),
    },
  };
}

describe("inspectSubject", () => {
  it("keeps direct, team and organization-wide sources apart", () => {
    const inspection = inspectSubject(
      alice,
      [
        attached(direct, alice.grn),
        attached(denyDeletes, engineering.grn),
        attached(baselineRead),
        // Unattached but a draft: evaluated by nothing, so not
        // organization-wide.
        attached(draft),
      ],
      { status: "ok", data: [membersOf(engineering, alice.grn)] },
    );

    expect(inspection.direct).toEqual([direct]);
    expect(inspection.inherited).toEqual({
      via: [{ policy: denyDeletes, team: engineering }],
      uncheckedTeams: [],
    });
    expect(inspection.orgWide).toEqual([baselineRead]);
    expect(inspection.unread).toEqual([]);
  });

  it("calls a policy whose attachments were refused unread, not organization-wide", () => {
    // The control: read, the same policy with no attachment reaches
    // everyone.
    expect(
      inspectSubject(alice, [attached(baselineRead)], {
        status: "ok",
        data: [],
      }).orgWide,
    ).toEqual([baselineRead]);

    const inspection = inspectSubject(
      alice,
      [{ policy: baselineRead, attachments: { status: "forbidden" } }],
      { status: "ok", data: [] },
    );

    expect(inspection.orgWide).toEqual([]);
    expect(inspection.direct).toEqual([]);
    expect(inspection.unread).toEqual([baselineRead]);
  });

  it("names a team whose members were refused as unchecked, not as no team", () => {
    const scopes = [attached(denyDeletes, engineering.grn)];
    // The control: members read, the team's policy reaches Alice.
    expect(
      inspectSubject(alice, scopes, {
        status: "ok",
        data: [membersOf(engineering, alice.grn)],
      }).inherited?.via,
    ).toEqual([{ policy: denyDeletes, team: engineering }]);

    const inspection = inspectSubject(alice, scopes, {
      status: "ok",
      data: [{ team: engineering, members: { status: "forbidden" } }],
    });

    expect(inspection.inherited).toEqual({
      via: [],
      uncheckedTeams: [engineering],
    });
  });

  it("says inheritance is unchecked where the teams list was refused", () => {
    const scopes = [attached(denyDeletes, engineering.grn)];
    // The control: with the teams listed, the same policy reaches Alice
    // through Engineering.
    expect(
      inspectSubject(alice, scopes, {
        status: "ok",
        data: [membersOf(engineering, alice.grn)],
      }).inherited,
    ).toEqual({
      via: [{ policy: denyDeletes, team: engineering }],
      uncheckedTeams: [],
    });

    const inspection = inspectSubject(alice, scopes, { status: "forbidden" });

    expect(inspection.inherited).toEqual({ via: [], uncheckedTeams: null });
  });

  it("gives a team no inherited source, since a team is in no team", () => {
    const team: Subject = {
      grn: engineering.grn,
      kind: "team",
      identifier: "eng",
      label: "Engineering",
    };

    const inspection = inspectSubject(
      team,
      [attached(denyDeletes, engineering.grn)],
      { status: "ok", data: [] },
    );

    expect(inspection.direct).toEqual([denyDeletes]);
    expect(inspection).not.toHaveProperty("inherited");
  });
});

describe("resolveSubjectQuery", () => {
  it("finds a subject by kind and identifier, never by GRN", () => {
    expect(resolveSubjectQuery("team:eng", catalogs)).toEqual({
      status: "found",
      subject: {
        grn: engineering.grn,
        kind: "team",
        identifier: "eng",
        label: "Engineering",
      },
    });
    expect(resolveSubjectQuery(engineering.grn, catalogs)).toEqual({
      status: "missing",
    });
    expect(resolveSubjectQuery("person:eng", catalogs)).toEqual({
      status: "missing",
    });
  });

  it("says a subject whose list was refused is refused, not missing", () => {
    // The control: the same query against the readable list is found.
    expect(resolveSubjectQuery("person:alice", catalogs).status).toBe("found");

    expect(
      resolveSubjectQuery("person:alice", {
        ...catalogs,
        people: { status: "forbidden" },
      }),
    ).toEqual({ status: "refused", kind: "person" });
  });
});

describe("describeSubject", () => {
  it("marks an attachment nobody holds missing, and a refused list unchecked", () => {
    expect(describeSubject(engineering.grn, catalogs)).toMatchObject({
      kind: "team",
      label: "Engineering",
      lookup: "found",
    });
    expect(
      describeSubject("grn:acme:default:group:gone", catalogs),
    ).toMatchObject({ kind: "team", label: null, lookup: "missing" });
    expect(
      describeSubject(engineering.grn, {
        ...catalogs,
        teams: { status: "forbidden" },
      }),
    ).toMatchObject({ kind: "team", label: null, lookup: "refused" });
  });
});

describe("reachOf", () => {
  it("reads no attachment as everyone and a refused list as unknown", () => {
    expect(reachOf({ status: "ok", data: [] }, catalogs)).toEqual({
      kind: "org-wide",
    });
    expect(reachOf({ status: "forbidden" }, catalogs)).toEqual({
      kind: "unknown",
    });
  });
});

describe("agent subjects", () => {
  it("offers an agent the organization calls nothing by its identifier alone (ADR-0119)", () => {
    const agents = listSubjects(catalogs).filter(
      (subject) => subject.kind === "agent",
    );

    expect(agents.map((subject) => subject.label)).toEqual([
      "1c3d5e7f90a2b4c6",
    ]);
    expect(describeSubject(reportedAgent.grn, catalogs)).toMatchObject({
      kind: "agent",
      label: "1c3d5e7f90a2b4c6",
      lookup: "found",
    });
  });

  it("says an agent is unchecked where the agents list was refused, and missing where it was read", () => {
    const refused: Catalogs = { ...catalogs, agents: { status: "forbidden" } };

    expect(describeSubject(reportedAgent.grn, refused)).toMatchObject({
      lookup: "refused",
    });
    expect(resolveSubjectQuery("agent:1c3d5e7f90a2b4c6", refused)).toEqual({
      status: "refused",
      kind: "agent",
    });
    // The control: with the list read, an agent it does not hold is
    // missing.
    expect(
      describeSubject("grn:acme:default:agent:0000000000000000", catalogs)
        .lookup,
    ).toBe("missing");
  });
});
