-- Where a login lands once its callback completes (#1380): the console
-- path the visitor was going to when they chose to sign in with a
-- provider, so an invitation link survives the round trip. It is held
-- on the state's row and nowhere the browser carries it, so it is
-- spent with the state, single-use, and gone when the state expires.
--
-- The path can name an invitation, whose token is a bearer secret; the
-- `invitations` table already holds that token as written, so this
-- row adds no secret a leaked database did not already hold. The check
-- is the rule the surface applies before it writes (a console path,
-- bounded), held again where nothing else writes it.
ALTER TABLE oauth_states
    ADD COLUMN return_path text
        CHECK (return_path IS NULL
            OR (char_length(return_path) <= 2048
                AND (return_path = '/console' OR return_path LIKE '/console/%')));
