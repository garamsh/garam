-- The declarations, their log, the reports and the bot tokens move
-- from the cluster to garam's bot domain, keyed by the instance's GRN
-- (ADR-0192 decision 10, #2082). Each table, its column naming the
-- instance, its constraints and its indexes take the bot domain's
-- names; no row changes. instance_contacts stays the app system's.

ALTER TABLE cluster_declaration_ids RENAME TO bot_declaration_ids;
ALTER TABLE bot_declaration_ids RENAME CONSTRAINT cluster_declaration_ids_pkey TO bot_declaration_ids_pkey;
ALTER TABLE bot_declaration_ids RENAME CONSTRAINT cluster_declaration_ids_shape TO bot_declaration_ids_shape;

ALTER TABLE cluster_declaration_heads RENAME TO bot_declaration_heads;
ALTER TABLE bot_declaration_heads RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE bot_declaration_heads RENAME CONSTRAINT cluster_declaration_heads_cluster_grn_fkey TO bot_declaration_heads_instance_grn_fkey;
ALTER TABLE bot_declaration_heads RENAME CONSTRAINT cluster_declaration_heads_order TO bot_declaration_heads_order;
ALTER TABLE bot_declaration_heads RENAME CONSTRAINT cluster_declaration_heads_pkey TO bot_declaration_heads_pkey;

ALTER TABLE cluster_declarations RENAME TO bot_declarations;
ALTER TABLE bot_declarations RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE bot_declarations RENAME CONSTRAINT cluster_declarations_cluster_grn_fkey TO bot_declarations_instance_grn_fkey;
ALTER TABLE bot_declarations RENAME CONSTRAINT cluster_declarations_generation TO bot_declarations_generation;
ALTER TABLE bot_declarations RENAME CONSTRAINT cluster_declarations_id_fkey TO bot_declarations_id_fkey;
ALTER TABLE bot_declarations RENAME CONSTRAINT cluster_declarations_object TO bot_declarations_object;
ALTER TABLE bot_declarations RENAME CONSTRAINT cluster_declarations_pkey TO bot_declarations_pkey;
ALTER INDEX cluster_declarations_cluster_idx RENAME TO bot_declarations_instance_idx;
ALTER INDEX cluster_declarations_live_object_idx RENAME TO bot_declarations_live_object_idx;

ALTER TABLE cluster_declaration_changes RENAME TO bot_declaration_changes;
ALTER TABLE bot_declaration_changes RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE bot_declaration_changes RENAME CONSTRAINT cluster_declaration_changes_cluster_grn_fkey TO bot_declaration_changes_instance_grn_fkey;
ALTER TABLE bot_declaration_changes RENAME CONSTRAINT cluster_declaration_changes_object TO bot_declaration_changes_object;
ALTER TABLE bot_declaration_changes RENAME CONSTRAINT cluster_declaration_changes_op TO bot_declaration_changes_op;
ALTER TABLE bot_declaration_changes RENAME CONSTRAINT cluster_declaration_changes_pkey TO bot_declaration_changes_pkey;
ALTER INDEX cluster_declaration_changes_written_at_idx RENAME TO bot_declaration_changes_written_at_idx;

ALTER TABLE cluster_reports RENAME TO bot_reports;
ALTER TABLE bot_reports RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE bot_reports RENAME CONSTRAINT cluster_reports_cluster_grn_fkey TO bot_reports_instance_grn_fkey;
ALTER TABLE bot_reports RENAME CONSTRAINT cluster_reports_pkey TO bot_reports_pkey;

ALTER TABLE cluster_bot_tokens RENAME TO bot_tokens;
ALTER TABLE bot_tokens RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE bot_tokens RENAME CONSTRAINT cluster_bot_tokens_cluster_grn_fkey TO bot_tokens_instance_grn_fkey;
ALTER TABLE bot_tokens RENAME CONSTRAINT cluster_bot_tokens_declaration_id_fkey TO bot_tokens_declaration_id_fkey;
ALTER TABLE bot_tokens RENAME CONSTRAINT cluster_bot_tokens_key_id_fkey TO bot_tokens_key_id_fkey;
ALTER TABLE bot_tokens RENAME CONSTRAINT cluster_bot_tokens_lifetime TO bot_tokens_lifetime;
ALTER TABLE bot_tokens RENAME CONSTRAINT cluster_bot_tokens_pkey TO bot_tokens_pkey;
ALTER INDEX cluster_bot_tokens_cluster_idx RENAME TO bot_tokens_instance_idx;
ALTER INDEX cluster_bot_tokens_declaration_idx RENAME TO bot_tokens_declaration_idx;
ALTER INDEX cluster_bot_tokens_key_idx RENAME TO bot_tokens_key_idx;

-- 000088 renamed cluster_keys to instance_keys and left its sequence;
-- the column's default follows the sequence by its identity.
ALTER SEQUENCE cluster_keys_id_seq RENAME TO instance_keys_id_seq;

-- The withdrawal of a key takes the bot tokens minted under it
-- (000088), now from their table under its new name.
CREATE OR REPLACE FUNCTION instance_key_withdrawn() RETURNS trigger
LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    DELETE FROM bot_tokens WHERE key_id = NEW.id;
    RETURN NULL;
END
$$;
