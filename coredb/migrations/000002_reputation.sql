-- +goose Up
-- Cached reputation score (task B5, PLANDOC.md §4/§5/§9 decision 3): the
-- count of trusted-domain endorsements a user's content has received.
-- Maintained transactionally by EndorsementService on endorse/retract —
-- never recomputed from scratch on read. Used only to order the endorser
-- list per-viewer; never to rank content.
CREATE TABLE user_reputation (
    user_id                    uuid NOT NULL PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    trusted_endorsement_count  integer NOT NULL DEFAULT 0,
    updated_at                 timestamptz DEFAULT timezone('utc', now()) NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS user_reputation;
