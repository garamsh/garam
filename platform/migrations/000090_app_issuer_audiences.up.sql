-- The audiences of the issuer's JWT an installation of the app
-- exchanges its app key's assertion for (ADR-0192 decision 7). Only
-- the platform writes them: an app never names an audience it reaches.
-- garam-bot-connector lists none, so its installations exchange for
-- nothing until a record lists one.
ALTER TABLE apps ADD COLUMN issuer_audiences text[] NOT NULL DEFAULT '{}';
