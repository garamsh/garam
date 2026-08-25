-- The certificate garam last issued for an operator, which is the only
-- one it will renew (ADR-0046). An operator renews by presenting the
-- certificate it holds and garam has no revocation, so this row is what
-- keeps a stolen certificate from renewing beside the operator it was
-- taken from: a successor moves the serial here, and every other
-- certificate that names the same operator stops being renewable at
-- once.
--
-- One row per operator, holding the current certificate alone. What
-- came before is not kept: the rule reads the newest and nothing else,
-- and a history nothing reads is a history that goes stale.
--
-- The serial is the certificate's serial number in decimal, as
-- `internal/ca` renders it. There is no foreign key to `operators`
-- (000017): that record grants an operator nothing and its absence
-- stops no certificate already issued, so a certificate is recorded for
-- a registered operator and an unregistered one alike. The check below
-- is what holds the GRN to an operator, as `agent_assignments` holds
-- its own (000007).
CREATE TABLE operator_certificates (
    operator_grn text PRIMARY KEY,
    serial       text NOT NULL,
    issued_at    timestamptz NOT NULL,

    CONSTRAINT operator_certificates_operator_service_type
        CHECK (split_part(operator_grn, ':', 4) = 'operator')
);
