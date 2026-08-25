-- 000010 said an operator is registered nowhere, so no foreign key
-- could say the GRN names one and a typo was a valid operator. 000017
-- registers one, so the foreign key exists now and that sentence is
-- retired for definitions. It still holds for `agent_assignments`
-- (000007): an operator holding a certificate creates an agent on the
-- machine listener whether or not anybody registered it, and requiring
-- registration there is a separate decision (ADR-0045).
--
-- NO ACTION rather than RESTRICT, which is load-bearing: deleting an
-- org cascades to `operators` and to `agent_definitions` in one
-- statement, and only NO ACTION defers its check to the end of that
-- statement, by which time the referencing rows are gone too.
--
-- NOT VALID, so the rows written before 000017 stay as they are.
-- Validating would need garam to either delete a definition somebody
-- wrote or invent a registration nobody made, and it will do neither:
-- an operator those rows name is registered by the organization that
-- owns it, or not at all.
ALTER TABLE agent_definitions
    ADD CONSTRAINT agent_definitions_operator_registered
    FOREIGN KEY (operator_grn) REFERENCES operators (grn)
    NOT VALID;
