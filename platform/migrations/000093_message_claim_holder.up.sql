-- A claim taken by a bot's mailbox poll names the key of the process
-- that took it (ADR-0194 decision 3), so a poll hands out again only
-- what its own process was handed: another process's claim lapses at
-- its lease rather than being taken from it. An agent's claim names
-- none, and nothing releases it.
ALTER TABLE messages ADD COLUMN claim_holder text
    CONSTRAINT messages_claim_holder_named CHECK (claim_holder <> '');
