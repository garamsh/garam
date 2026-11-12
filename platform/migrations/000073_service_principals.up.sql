-- One service principal per organization and first-party service: the
-- identity a service holds in the organization it acts for, GRN
-- grn:<org>/iam/service/<name> (ADR-0129 item 1, ADR-0142).
--
-- A principal is written with its organization, from the list of
-- first-party services the code holds, and cascades from it, so
-- deleting the organization removes every principal it had. The table
-- keeps no key: a service's public keys are one file the deployment
-- names, and a principal is the record those keys are valid for.
--
-- What this does to stored rows: it adds a principal named cowork to
-- each organization that exists now, and changes no existing row. An
-- organization created after it is given its principals by CreateOrg.
CREATE TABLE service_principals (
    grn        text PRIMARY KEY,
    org_grn    text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT service_principals_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'service'
           AND grn_org(grn) = grn_id(org_grn) AND grn_org(grn) <> '')
);

CREATE INDEX service_principals_org ON service_principals (org_grn);

INSERT INTO service_principals (grn, org_grn)
SELECT 'grn:' || grn_id(o.grn) || '/iam/service/cowork', o.grn
FROM orgs AS o;
