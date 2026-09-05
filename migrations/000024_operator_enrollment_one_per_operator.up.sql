-- One enrollment token per operator, so a re-mint cannot leave two
-- (ADR-0067).
--
-- Registering was the only minter when 000023 was written, and it
-- writes the record and the token in one transaction, so at most one
-- row per operator could ever exist and nothing had to say so. A route
-- that mints for an operator already registered removes that guarantee:
-- an insert beside the live row would leave one identity with two
-- credential paths, which is the case ADR-0067 refuses to decide.
--
-- The constraint is what makes that unrepresentable rather than
-- something each minting call has to remember. `MintOperatorEnrollment`
-- upserts on it, so the newest token replaces the operator's previous
-- one in the statement that writes it.
ALTER TABLE operator_enrollment_tokens
    ADD CONSTRAINT operator_enrollment_tokens_operator_unique UNIQUE (operator_grn);
