-- The certificate garam last issued for an operator, which is the only
-- one it will renew (ADR-0046). An operator renews by presenting the
-- certificate it holds, so this row is what keeps a stolen certificate
-- from renewing beside the operator it was taken from: a successor
-- moves the serial here, and every other certificate that names the
-- same operator stops being renewable at once. It narrows a theft and
-- does not end one — the row a successor replaces keeps authenticating
-- until it expires, and what ends a theft is deleting the operator
-- (ADR-0070), which removes this row along with the registration.
--
-- One row per operator, holding the current certificate alone. What
-- came before is not kept: the rule reads the newest and nothing else,
-- and a history nothing reads is a history that goes stale.
--
-- The serial is the certificate's serial number in decimal, as
-- `internal/ca` renders it. There is still no foreign key to
-- `operators` (000017), and the reason has narrowed rather than gone:
-- a certificate is recorded for a registered operator and an
-- unregistered one alike, which is what `garam issue-certificate`
-- needs, so this table cannot require the record. What has changed is
-- the other half of the original reason — an absent record now stops
-- the certificate from authenticating (ADR-0070), so it is only
-- issuing that this table stays independent of. Deleting an operator
-- therefore removes this row by naming it rather than by a cascade.
-- The check below is what holds the GRN to an operator, as
-- `agent_assignments` holds its own (000007).
CREATE TABLE operator_certificates (
    operator_grn text PRIMARY KEY,
    serial       text NOT NULL,
    issued_at    timestamptz NOT NULL,

    CONSTRAINT operator_certificates_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator')
);
