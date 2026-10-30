-- A row holding a cowork destination fails the narrower check, so it
-- goes first; a login's state lives ten minutes.
DELETE FROM oauth_states WHERE return_path LIKE '/handoff?app=cowork&next=%';
ALTER TABLE oauth_states DROP CONSTRAINT oauth_states_return_path_check;
ALTER TABLE oauth_states ADD CONSTRAINT oauth_states_return_path_check
    CHECK (return_path IS NULL
        OR (octet_length(return_path) <= 2048
            AND (return_path = '/console' OR return_path LIKE '/console/%')));
