CREATE TABLE users (
    grn        text PRIMARY KEY,
    nickname   text NOT NULL,
    roles      text[] NOT NULL DEFAULT '{}',
    policies   text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL
);

CREATE TABLE orgs (
    grn        text PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE groups (
    grn        text PRIMARY KEY,
    org_grn    text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    name       text NOT NULL,
    roles      text[] NOT NULL DEFAULT '{}',
    policies   text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE projects (
    grn        text PRIMARY KEY,
    org_grn    text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    name       text NOT NULL,
    roles      text[] NOT NULL DEFAULT '{}',
    policies   text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE group_members (
    group_grn  text NOT NULL REFERENCES groups (grn) ON DELETE CASCADE,
    member_grn text NOT NULL,
    PRIMARY KEY (group_grn, member_grn)
);

CREATE TABLE project_members (
    project_grn text NOT NULL REFERENCES projects (grn) ON DELETE CASCADE,
    member_grn  text NOT NULL,
    PRIMARY KEY (project_grn, member_grn)
);

CREATE TABLE org_members (
    org_grn    text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    user_grn   text NOT NULL REFERENCES users (grn) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (org_grn, user_grn)
);

CREATE TABLE invitations (
    token       text PRIMARY KEY,
    org_grn     text NOT NULL REFERENCES orgs (grn) ON DELETE CASCADE,
    role        text NOT NULL CHECK (role IN ('admin', 'member')),
    created_by  text NOT NULL REFERENCES users (grn),
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz NOT NULL,
    accepted_by text REFERENCES users (grn),
    accepted_at timestamptz
);

CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_grn   text NOT NULL REFERENCES users (grn) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
