-- A provider sign-in for cowork lands on the site's handoff for it
-- (ADR-0135), so the destination held with a login's state is a
-- console path or that handoff carrying a cowork path. The surface
-- applies the full rule before it writes and again when it reads; this
-- check holds the shape and the bound where nothing else writes it.
-- 000045 left the check unnamed, so it carries Postgres's name.
ALTER TABLE oauth_states DROP CONSTRAINT oauth_states_return_path_check;
ALTER TABLE oauth_states ADD CONSTRAINT oauth_states_return_path_check
    CHECK (return_path IS NULL
        OR (octet_length(return_path) <= 2048
            AND (return_path = '/console'
                OR return_path LIKE '/console/%'
                OR return_path LIKE '/handoff?app=cowork&next=%')));
