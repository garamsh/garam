-- A token's audience is an operator the organization registered
-- (ADR-0100): the foreign keys refuse a mint or an exchange naming any
-- other, and deleting the hosted operator takes the tokens minted to be
-- shown to it. They are NOT VALID: G1a's exchange accepted an audience
-- nobody registered, and a row it wrote is left as it is rather than
-- deleted here; it expires within the hour either way.
ALTER TABLE operator_tokens
    ADD CONSTRAINT operator_tokens_audience_registered
        FOREIGN KEY (audience_grn) REFERENCES operators (grn) ON DELETE CASCADE NOT VALID;

ALTER TABLE agent_tokens
    ADD CONSTRAINT agent_tokens_audience_registered
        FOREIGN KEY (audience_grn) REFERENCES operators (grn) ON DELETE CASCADE NOT VALID;
