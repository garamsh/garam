-- What an organization calls its operator and what it says the
-- operator is for (ADR-0074). Both are the organization's words about
-- the record and nothing else: the GRN stays the operator's identity,
-- the subject its certificate names, and the key every other table
-- joins on, so editing either column touches no credential, no
-- enrollment token and no definition that names the operator.
--
-- A registration made before this migration carries no name, so it is
-- given its identifier, which is the name the console showed it by
-- until now. The identifier is the GRN's last segment, which 000017's
-- ownership check already reads the other segments of.
ALTER TABLE operators
    ADD COLUMN name text,
    ADD COLUMN description text NOT NULL DEFAULT '';

UPDATE operators SET name = split_part(grn, ':', 5);

-- internal/operator refuses both bounds too; this is the copy no call
-- path routes around, as 000017 says of its own check.
ALTER TABLE operators
    ALTER COLUMN name SET NOT NULL,
    ADD CONSTRAINT operators_name_length
        CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    ADD CONSTRAINT operators_description_length
        CHECK (char_length(description) <= 1000);
