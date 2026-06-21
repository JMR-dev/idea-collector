-- 0001_init: projects, users (auth_code is PK + login credential), submissions (outbox).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE projects (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug              text UNIQUE NOT NULL,
    name              text NOT NULL,
    github_owner      text NOT NULL,
    github_repo       text NOT NULL,
    github_project_id text NOT NULL,          -- ProjectV2 node id (PVT_...)
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    auth_code    text PRIMARY KEY,            -- login credential (global PK)
    project_id   uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    display_name text NOT NULL,               -- goes into the issue body
    email        text,
    revoked_at   timestamptz,                 -- soft disable
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX users_project_id_idx ON users (project_id);

CREATE TABLE submissions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_code           text NOT NULL REFERENCES users(auth_code),
    project_id          uuid NOT NULL REFERENCES projects(id),
    title               text NOT NULL,
    body                text NOT NULL,
    status              text NOT NULL DEFAULT 'pending',  -- pending|created|failed
    github_issue_number integer,
    github_issue_url    text,
    github_node_id      text,
    error               text,
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX submissions_project_created_idx ON submissions (project_id, created_at);
CREATE INDEX submissions_status_idx ON submissions (status) WHERE status <> 'created';
