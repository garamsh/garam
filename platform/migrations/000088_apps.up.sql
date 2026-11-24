-- Every system that connects to garam is an app, installed in an
-- organization, and each running copy of it is an instance of that
-- installation (ADR-0192). A connected cluster's record becomes an
-- instance: the cluster tables of 000082 to 000084 are renamed, and an
-- instance names the installation it is a copy of. No cluster record
-- was ever registered outside a test (#2079 §Decisions), so the rows
-- go before the rename rather than being moved.
--
-- The declarations, reports and bot tokens of 000085 to 000087 stay
-- under their names, referencing the renamed table, until the bot
-- domain takes them (#2082).

TRUNCATE clusters CASCADE;

-- An app, registered by the platform (org_grn NULL, owner `root`) or,
-- later, by an organization (#2083). Every difference between apps is
-- a column here that every path reads, and none is tested by slug
-- (ADR-0192 decision 2).
CREATE TABLE apps (
    grn                  text PRIMARY KEY,
    slug                 text NOT NULL UNIQUE,
    org_grn              text REFERENCES orgs (grn) ON DELETE CASCADE,
    name                 text NOT NULL,
    description          text NOT NULL DEFAULT '',
    visibility           text NOT NULL,
    -- Single action ids the manifests declare; a group or a wildcard
    -- is refused where an app is registered (#2083).
    requested_actions    text[] NOT NULL DEFAULT '{}',
    requests_bot_hosting boolean NOT NULL DEFAULT false,
    instances            text NOT NULL,
    delivery             text NOT NULL,
    -- The machine listener's routes, as its router matches them
    -- (`<METHOD> <path>`), that an instance's token reaches beyond the
    -- app system's own. Only the platform writes them.
    machine_operations   text[] NOT NULL DEFAULT '{}',
    created_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT apps_slug_shape CHECK (slug ~ '^[a-z0-9-]{3,39}$'),
    CONSTRAINT apps_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'app' AND grn_id(grn) = slug
           AND grn_org(grn) = COALESCE(grn_id(org_grn), 'root')),
    CONSTRAINT apps_name_length CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT apps_description_length CHECK (char_length(description) <= 1000),
    CONSTRAINT apps_visibility CHECK (visibility IN ('private', 'public')),
    CONSTRAINT apps_instances CHECK (instances IN ('none', 'one', 'many')),
    -- `webhook` arrives with its URL (#2083).
    CONSTRAINT apps_delivery CHECK (delivery = 'pull')
);

CREATE INDEX apps_org_idx ON apps (org_grn);

-- One app installed in one organization, at most once, named by the
-- app's slug. Its scope is the whole organization, or the projects
-- project_grns names. incarnation tells a reinstall from the
-- installation it replaces, which shares its GRN.
CREATE TABLE installations (
    grn          text PRIMARY KEY,
    org_grn      text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    app_grn      text NOT NULL REFERENCES apps (grn) ON DELETE CASCADE,
    incarnation  uuid NOT NULL DEFAULT gen_random_uuid(),
    scope        text NOT NULL,
    project_grns text[] NOT NULL DEFAULT '{}',
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL,

    CONSTRAINT installations_app_unique UNIQUE (org_grn, app_grn),
    CONSTRAINT installations_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'installation'
           AND grn_org(grn) = grn_id(org_grn) AND grn_id(grn) = grn_id(app_grn)),
    CONSTRAINT installations_scope CHECK (scope IN ('organization', 'projects')),
    CONSTRAINT installations_scope_projects
        CHECK ((scope = 'projects') = (cardinality(project_grns) > 0))
);

CREATE INDEX installations_app_idx ON installations (app_grn);

-- garam-bot-connector, the connector garam-bot's cluster chart and
-- daemon run (ADR-0192 decision 3). It requests no action: what its
-- instances reach is the cluster-connection routes, its machine
-- operations, so installing it writes no permit.
INSERT INTO apps (grn, slug, org_grn, name, description, visibility, requested_actions,
                  requests_bot_hosting, instances, delivery, machine_operations)
VALUES ('grn:root/iam/app/garam-bot-connector', 'garam-bot-connector', NULL,
        'garam-bot connector',
        'Connects a cluster, a PC or any host garam-bot runs on, and applies what garam declares for it.',
        'public', '{}', true, 'many', 'pull',
        ARRAY['GET /cluster-connection/v1alpha1/clusters/self/declarations',
              'POST /cluster-connection/v1alpha1/clusters/self/reports']);

ALTER TABLE clusters RENAME TO instances;
ALTER TABLE instances RENAME CONSTRAINT clusters_pkey TO instances_pkey;
ALTER TABLE instances RENAME CONSTRAINT clusters_org_grn_fkey TO instances_org_grn_fkey;
ALTER TABLE instances RENAME CONSTRAINT clusters_name_length TO instances_name_length;
ALTER TABLE instances RENAME CONSTRAINT clusters_description_length TO instances_description_length;
ALTER INDEX clusters_org_idx RENAME TO instances_org_idx;
ALTER TABLE instances DROP CONSTRAINT clusters_grn_ownership;
-- The table is empty, so the new columns need no default to backfill.
ALTER TABLE instances
    ADD COLUMN installation_grn text NOT NULL REFERENCES installations (grn) ON DELETE CASCADE,
    ADD COLUMN kind text NOT NULL DEFAULT '',
    ADD CONSTRAINT instances_grn_ownership
        CHECK (grn_service(grn) = 'iam' AND grn_type(grn) = 'instance'
           AND grn_org(grn) = grn_id(org_grn) AND grn_org(grn) <> ''
           AND grn_org(installation_grn) = grn_org(grn)),
    -- internal/app holds the same bound, in characters.
    ADD CONSTRAINT instances_kind_length CHECK (char_length(kind) <= 64);

CREATE INDEX instances_installation_idx ON instances (installation_grn);

ALTER TABLE cluster_registration_tokens RENAME TO instance_registration_tokens;
ALTER TABLE instance_registration_tokens RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE instance_registration_tokens
    RENAME CONSTRAINT cluster_registration_tokens_pkey TO instance_registration_tokens_pkey;
ALTER TABLE instance_registration_tokens
    RENAME CONSTRAINT cluster_registration_tokens_cluster_grn_fkey TO instance_registration_tokens_instance_grn_fkey;
ALTER TABLE instance_registration_tokens
    RENAME CONSTRAINT cluster_registration_tokens_token_hash_key TO instance_registration_tokens_token_hash_key;
ALTER INDEX cluster_registration_tokens_expires_at_idx RENAME TO instance_registration_tokens_expires_at_idx;

ALTER TABLE cluster_keys RENAME TO instance_keys;
ALTER TABLE instance_keys RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE instance_keys RENAME CONSTRAINT cluster_keys_pkey TO instance_keys_pkey;
ALTER TABLE instance_keys RENAME CONSTRAINT cluster_keys_cluster_grn_fkey TO instance_keys_instance_grn_fkey;
ALTER TABLE instance_keys RENAME CONSTRAINT cluster_keys_predecessor_id_fkey TO instance_keys_predecessor_id_fkey;
ALTER TABLE instance_keys RENAME CONSTRAINT cluster_keys_kid_unique TO instance_keys_kid_unique;
ALTER INDEX cluster_keys_predecessor_id_idx RENAME TO instance_keys_predecessor_id_idx;

ALTER TABLE cluster_tokens RENAME TO instance_tokens;
ALTER TABLE instance_tokens RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE instance_tokens RENAME CONSTRAINT cluster_tokens_pkey TO instance_tokens_pkey;
ALTER TABLE instance_tokens RENAME CONSTRAINT cluster_tokens_cluster_grn_fkey TO instance_tokens_instance_grn_fkey;
ALTER TABLE instance_tokens RENAME CONSTRAINT cluster_tokens_key_id_fkey TO instance_tokens_key_id_fkey;
ALTER INDEX cluster_tokens_key_id_idx RENAME TO instance_tokens_key_id_idx;
ALTER INDEX cluster_tokens_cluster_grn_expires_at_idx RENAME TO instance_tokens_instance_grn_expires_at_idx;

ALTER TABLE cluster_jtis RENAME TO instance_jtis;
ALTER TABLE instance_jtis RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE instance_jtis RENAME CONSTRAINT cluster_jtis_pkey TO instance_jtis_pkey;
ALTER INDEX cluster_jtis_expires_at_idx RENAME TO instance_jtis_expires_at_idx;

-- The time a record's token last resolved is the instance's, written
-- by internal/app as its token resolves.
ALTER TABLE cluster_contacts RENAME TO instance_contacts;
ALTER TABLE instance_contacts RENAME COLUMN cluster_grn TO instance_grn;
ALTER TABLE instance_contacts RENAME CONSTRAINT cluster_contacts_pkey TO instance_contacts_pkey;
ALTER TABLE instance_contacts
    RENAME CONSTRAINT cluster_contacts_cluster_grn_fkey TO instance_contacts_instance_grn_fkey;

-- A bot's token goes with the withdrawal of the instance key its
-- actor's token was exchanged under, as it goes with the key's
-- deletion through its foreign key. Withdrawing is an update, which a
-- foreign key does not follow, so the trigger is that cascade, and
-- internal/app's withdrawals need not know the bot tokens exist.
CREATE FUNCTION instance_key_withdrawn() RETURNS trigger
LANGUAGE plpgsql VOLATILE AS $$
BEGIN
    DELETE FROM cluster_bot_tokens WHERE key_id = NEW.id;
    RETURN NULL;
END
$$;

CREATE TRIGGER instance_keys_withdrawn
    AFTER UPDATE OF revoked_at ON instance_keys
    FOR EACH ROW
    WHEN (OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL)
    EXECUTE FUNCTION instance_key_withdrawn();
