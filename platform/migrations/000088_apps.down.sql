TRUNCATE instances CASCADE;

DROP TRIGGER instance_keys_withdrawn ON instance_keys;
DROP FUNCTION instance_key_withdrawn();

ALTER TABLE instance_contacts
    RENAME CONSTRAINT instance_contacts_instance_grn_fkey TO cluster_contacts_cluster_grn_fkey;
ALTER TABLE instance_contacts RENAME CONSTRAINT instance_contacts_pkey TO cluster_contacts_pkey;
ALTER TABLE instance_contacts RENAME COLUMN instance_grn TO cluster_grn;
ALTER TABLE instance_contacts RENAME TO cluster_contacts;

ALTER INDEX instance_jtis_expires_at_idx RENAME TO cluster_jtis_expires_at_idx;
ALTER TABLE instance_jtis RENAME CONSTRAINT instance_jtis_pkey TO cluster_jtis_pkey;
ALTER TABLE instance_jtis RENAME COLUMN instance_grn TO cluster_grn;
ALTER TABLE instance_jtis RENAME TO cluster_jtis;

ALTER INDEX instance_tokens_instance_grn_expires_at_idx RENAME TO cluster_tokens_cluster_grn_expires_at_idx;
ALTER INDEX instance_tokens_key_id_idx RENAME TO cluster_tokens_key_id_idx;
ALTER TABLE instance_tokens RENAME CONSTRAINT instance_tokens_key_id_fkey TO cluster_tokens_key_id_fkey;
ALTER TABLE instance_tokens RENAME CONSTRAINT instance_tokens_instance_grn_fkey TO cluster_tokens_cluster_grn_fkey;
ALTER TABLE instance_tokens RENAME CONSTRAINT instance_tokens_pkey TO cluster_tokens_pkey;
ALTER TABLE instance_tokens RENAME COLUMN instance_grn TO cluster_grn;
ALTER TABLE instance_tokens RENAME TO cluster_tokens;

ALTER INDEX instance_keys_predecessor_id_idx RENAME TO cluster_keys_predecessor_id_idx;
ALTER TABLE instance_keys RENAME CONSTRAINT instance_keys_kid_unique TO cluster_keys_kid_unique;
ALTER TABLE instance_keys RENAME CONSTRAINT instance_keys_predecessor_id_fkey TO cluster_keys_predecessor_id_fkey;
ALTER TABLE instance_keys RENAME CONSTRAINT instance_keys_instance_grn_fkey TO cluster_keys_cluster_grn_fkey;
ALTER TABLE instance_keys RENAME CONSTRAINT instance_keys_pkey TO cluster_keys_pkey;
ALTER TABLE instance_keys RENAME COLUMN instance_grn TO cluster_grn;
ALTER TABLE instance_keys RENAME TO cluster_keys;

ALTER INDEX instance_registration_tokens_expires_at_idx RENAME TO cluster_registration_tokens_expires_at_idx;
ALTER TABLE instance_registration_tokens
    RENAME CONSTRAINT instance_registration_tokens_token_hash_key TO cluster_registration_tokens_token_hash_key;
ALTER TABLE instance_registration_tokens
    RENAME CONSTRAINT instance_registration_tokens_instance_grn_fkey TO cluster_registration_tokens_cluster_grn_fkey;
ALTER TABLE instance_registration_tokens
    RENAME CONSTRAINT instance_registration_tokens_pkey TO cluster_registration_tokens_pkey;
ALTER TABLE instance_registration_tokens RENAME COLUMN instance_grn TO cluster_grn;
ALTER TABLE instance_registration_tokens RENAME TO cluster_registration_tokens;

DROP INDEX instances_installation_idx;
ALTER TABLE instances
    DROP CONSTRAINT instances_kind_length,
    DROP CONSTRAINT instances_grn_ownership,
    DROP COLUMN kind,
    DROP COLUMN installation_grn,
    ADD CONSTRAINT clusters_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'cluster'
           AND grn_org(grn) = grn_id(org_grn) AND grn_org(grn) <> '');
ALTER INDEX instances_org_idx RENAME TO clusters_org_idx;
ALTER TABLE instances RENAME CONSTRAINT instances_description_length TO clusters_description_length;
ALTER TABLE instances RENAME CONSTRAINT instances_name_length TO clusters_name_length;
ALTER TABLE instances RENAME CONSTRAINT instances_org_grn_fkey TO clusters_org_grn_fkey;
ALTER TABLE instances RENAME CONSTRAINT instances_pkey TO clusters_pkey;
ALTER TABLE instances RENAME TO clusters;

DROP TABLE installations;
DROP TABLE apps;
