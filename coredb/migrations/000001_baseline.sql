-- +goose Up
-- Used for ULID generation (gen_random_bytes)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Materialized-path type used by comments.path for one-query subtree fetch.
CREATE EXTENSION IF NOT EXISTS ltree;

-- The ULID generator: a 48-bit millisecond timestamp prefix + 80 bits of
-- randomness, formatted as a uuid so it drops straight into `uuid` PK
-- columns. Ported verbatim from longhouse's coredb baseline.
CREATE OR REPLACE FUNCTION generate_ulid() RETURNS uuid
    AS $$ SELECT (lpad(to_hex(floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint), 12, '0') || encode(gen_random_bytes(10), 'hex'))::uuid $$
    LANGUAGE SQL;

-- Fixed, small vocabularies get real Postgres enums (longhouse's
-- task_status precedent); polymorphic target_type/target_id pairs used
-- across several tables get a plain `text` column with a CHECK constraint
-- scoped to that table (longhouse's comments.target_type precedent) since
-- the allowed value set differs per table.
CREATE TYPE user_kind AS ENUM ('human', 'system');
CREATE TYPE board_kind AS ENUM ('discussion', 'announce');
CREATE TYPE board_member_role AS ENUM ('maintainer', 'moderator');
CREATE TYPE mention_policy AS ENUM ('subscribed', 'everyone', 'authorized', 'nobody');
CREATE TYPE content_origin AS ENUM ('user', 'github', 'system');
CREATE TYPE notification_event AS ENUM ('new_post', 'new_comment', 'mention', 'github_event');
CREATE TYPE github_thread_mode AS ENUM ('post_per_issue', 'post_per_release', 'post_per_pull_request');

-- Users are local records cached from linkkeys identity assertions, plus
-- system actors (GitHub ingestion, etc).
CREATE TABLE users (
    id                uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    linkkeys_domain   text NOT NULL,
    linkkeys_user_id  text NOT NULL,
    handle            text NOT NULL,
    display_name      text NOT NULL DEFAULT '',
    kind              user_kind NOT NULL DEFAULT 'human',
    roles             text[] NOT NULL DEFAULT ARRAY[]::text[],
    created_at        timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    UNIQUE (linkkeys_domain, linkkeys_user_id)
);
CREATE UNIQUE INDEX users_handle_idx ON users(handle);

-- Boards are per-project/topic containers for posts.
CREATE TABLE boards (
    id           uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    slug         text NOT NULL,
    title        text NOT NULL,
    description  text NOT NULL DEFAULT '',
    kind         board_kind NOT NULL DEFAULT 'discussion',
    created_by   uuid NOT NULL REFERENCES users ON DELETE SET NULL,
    archived_at  timestamptz,
    created_at   timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    UNIQUE (slug)
);

-- Per-user notification/mention settings. 1:1 with users.
CREATE TABLE user_settings (
    user_id            uuid NOT NULL PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    mention_policy     mention_policy NOT NULL DEFAULT 'subscribed',
    notify_on_endorse  boolean NOT NULL DEFAULT true,
    updated_at         timestamptz DEFAULT timezone('utc', now()) NOT NULL
);

-- Always-allowed mention-notify grants: granted_user_id may always
-- @mention-notify user_id, regardless of subscribed-context overlap.
CREATE TABLE mention_grants (
    user_id          uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    granted_user_id  uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    created_at       timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    PRIMARY KEY (user_id, granted_user_id)
);

-- Friend groups are private to their owner; used to order endorser lists.
CREATE TABLE friend_groups (
    id          uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    owner_id    uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    name        text NOT NULL,
    created_at  timestamptz DEFAULT timezone('utc', now()) NOT NULL
);
CREATE INDEX friend_groups_owner_id_idx ON friend_groups(owner_id);

CREATE TABLE friend_group_members (
    group_id        uuid NOT NULL REFERENCES friend_groups ON DELETE CASCADE,
    member_user_id  uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    created_at      timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    PRIMARY KEY (group_id, member_user_id)
);

-- Per-board maintainer/moderator role assignments.
CREATE TABLE board_members (
    board_id    uuid NOT NULL REFERENCES boards ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    role        board_member_role NOT NULL,
    created_at  timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    PRIMARY KEY (board_id, user_id)
);

-- Instance-level, admin-managed linkkeys domains whose endorsements earn
-- reputation.
CREATE TABLE trusted_domains (
    domain      text NOT NULL PRIMARY KEY,
    added_by    uuid NOT NULL REFERENCES users ON DELETE SET NULL,
    created_at  timestamptz DEFAULT timezone('utc', now()) NOT NULL
);

-- Posts are thread roots.
CREATE TABLE posts (
    id                uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    board_id          uuid NOT NULL REFERENCES boards ON DELETE CASCADE,
    author_id         uuid NOT NULL REFERENCES users ON DELETE SET NULL,
    title             text NOT NULL,
    body_md           text NOT NULL DEFAULT '',
    origin            content_origin NOT NULL DEFAULT 'user',
    origin_ref        jsonb,
    comment_count     integer NOT NULL DEFAULT 0,
    last_activity_at  timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    edited_at         timestamptz,
    deleted_at        timestamptz,
    created_at        timestamptz DEFAULT timezone('utc', now()) NOT NULL
);
CREATE INDEX posts_board_id_idx ON posts(board_id);
CREATE INDEX posts_board_id_last_activity_at_idx ON posts(board_id, last_activity_at DESC);

-- Comments are threaded replies. parent_comment_id carries structure;
-- path (ltree) is a materialized path (each ancestor's ULID as a label)
-- enabling one indexed query for a whole subtree, ordered depth-first.
CREATE TABLE comments (
    id                 uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    post_id            uuid NOT NULL REFERENCES posts ON DELETE CASCADE,
    parent_comment_id  uuid REFERENCES comments ON DELETE CASCADE,
    author_id          uuid NOT NULL REFERENCES users ON DELETE SET NULL,
    path               ltree NOT NULL,
    body_md            text NOT NULL DEFAULT '',
    origin             content_origin NOT NULL DEFAULT 'user',
    origin_ref         jsonb,
    edited_at          timestamptz,
    deleted_at         timestamptz,
    created_at         timestamptz DEFAULT timezone('utc', now()) NOT NULL
);
CREATE INDEX comments_path_gist_idx ON comments USING GIST (path);
CREATE INDEX comments_post_id_created_at_idx ON comments(post_id, created_at);

-- Snapshot of prior content taken BEFORE each edit (post or comment).
CREATE TABLE revisions (
    id            uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    target_type   text NOT NULL CHECK (target_type IN ('post', 'comment')),
    target_id     uuid NOT NULL,
    editor_id     uuid NOT NULL REFERENCES users ON DELETE SET NULL,
    prev_title    text,
    prev_body_md  text NOT NULL,
    created_at    timestamptz DEFAULT timezone('utc', now()) NOT NULL
);
CREATE INDEX revisions_target_idx ON revisions(target_type, target_id, created_at);

-- Endorsements: retract = delete row.
CREATE TABLE endorsements (
    id           uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    target_type  text NOT NULL CHECK (target_type IN ('post', 'comment')),
    target_id    uuid NOT NULL,
    created_at   timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    UNIQUE (user_id, target_type, target_id)
);
CREATE INDEX endorsements_target_idx ON endorsements(target_type, target_id);

-- Subscriptions to a board, a post, or a comment subtree. muted carves
-- holes out of a broader subscription.
CREATE TABLE subscriptions (
    id           uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    target_type  text NOT NULL CHECK (target_type IN ('board', 'post', 'comment')),
    target_id    uuid NOT NULL,
    muted        boolean NOT NULL DEFAULT false,
    created_at   timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    UNIQUE (user_id, target_type, target_id)
);
CREATE INDEX subscriptions_target_idx ON subscriptions(target_type, target_id);

-- Per-post read watermark, advanced on view.
CREATE TABLE read_marks (
    user_id        uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    post_id        uuid NOT NULL REFERENCES posts ON DELETE CASCADE,
    last_read_at   timestamptz NOT NULL DEFAULT timezone('utc', now()),
    PRIMARY KEY (user_id, post_id)
);

-- Explicit "keep this unread" pins; beat the watermark until cleared.
CREATE TABLE unread_overrides (
    user_id      uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    target_type  text NOT NULL CHECK (target_type IN ('post', 'comment')),
    target_id    uuid NOT NULL,
    created_at   timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);

-- Fan-out targets. subscription_id/post_id are convenience denormalizations
-- for the inbox query; target_type/target_id is the thing that triggered it.
CREATE TABLE notifications (
    id               uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    user_id          uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    event            notification_event NOT NULL,
    subscription_id  uuid REFERENCES subscriptions ON DELETE SET NULL,
    actor_id         uuid REFERENCES users ON DELETE SET NULL,
    target_type      text NOT NULL CHECK (target_type IN ('board', 'post', 'comment')),
    target_id        uuid NOT NULL,
    post_id          uuid REFERENCES posts ON DELETE CASCADE,
    read_at          timestamptz,
    created_at       timestamptz DEFAULT timezone('utc', now()) NOT NULL
);
CREATE INDEX notifications_user_id_read_at_created_at_idx ON notifications(user_id, read_at, created_at DESC);

-- Per-repo GitHub webhook routing into a board.
CREATE TABLE github_mappings (
    id           uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    board_id     uuid NOT NULL REFERENCES boards ON DELETE CASCADE,
    repo         text NOT NULL,
    events       text[] NOT NULL DEFAULT ARRAY[]::text[],
    secret_ref   text NOT NULL,
    thread_mode  github_thread_mode NOT NULL DEFAULT 'post_per_issue',
    created_by   uuid NOT NULL REFERENCES users ON DELETE SET NULL,
    created_at   timestamptz DEFAULT timezone('utc', now()) NOT NULL,
    UNIQUE (repo)
);
CREATE INDEX github_mappings_board_id_idx ON github_mappings(board_id);

-- Firepit's own sessions, minted after a successful linkkeys RP handshake.
CREATE TABLE sessions (
    id           uuid DEFAULT generate_ulid() NOT NULL PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users ON DELETE CASCADE,
    token_hash   text NOT NULL,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz DEFAULT timezone('utc', now()) NOT NULL
);
CREATE UNIQUE INDEX sessions_token_hash_idx ON sessions(token_hash);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);

-- +goose Down
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS github_mappings;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS unread_overrides;
DROP TABLE IF EXISTS read_marks;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS endorsements;
DROP TABLE IF EXISTS revisions;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS trusted_domains;
DROP TABLE IF EXISTS board_members;
DROP TABLE IF EXISTS friend_group_members;
DROP TABLE IF EXISTS friend_groups;
DROP TABLE IF EXISTS mention_grants;
DROP TABLE IF EXISTS user_settings;
DROP TABLE IF EXISTS boards;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS github_thread_mode;
DROP TYPE IF EXISTS notification_event;
DROP TYPE IF EXISTS content_origin;
DROP TYPE IF EXISTS mention_policy;
DROP TYPE IF EXISTS board_member_role;
DROP TYPE IF EXISTS board_kind;
DROP TYPE IF EXISTS user_kind;

DROP FUNCTION IF EXISTS generate_ulid();
