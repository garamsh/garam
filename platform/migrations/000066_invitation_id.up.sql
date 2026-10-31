-- An invitation's token is the bearer secret that admits its holder,
-- so a list of the pending ones cannot name them by it. id names one to
-- the organization that issued it and admits no one (ADR-0136).
ALTER TABLE invitations
    ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD CONSTRAINT invitations_id_unique UNIQUE (id);
