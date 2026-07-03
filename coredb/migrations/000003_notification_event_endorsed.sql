-- +goose Up
-- Endorsement notifications (PLANDOC.md §4 "Endorsement notifications",
-- §9 decision 3: "author notified (opt-out via notify_on_endorse)") need a
-- distinct event tag on the notifications row it produces. The baseline
-- migration's notification_event enum only defined
-- new_post/new_comment/mention/github_event and missed this case — task B7
-- (notification fan-out) surfaced the gap while wiring KindEndorsed. This is
-- purely additive: no existing column, row, or other enum value changes.
ALTER TYPE notification_event ADD VALUE 'endorsed';

-- +goose Down
-- Postgres has no ALTER TYPE ... DROP VALUE, so undoing this means
-- recreating the enum without the value and re-pointing the one column that
-- uses it. This only runs as part of a full goose down-to-zero (see
-- coredb.Reset, exercised by api/internal/store's round-trip integration
-- test), so failing loudly via the USING cast below if some row still
-- carries 'endorsed' at that point is the right failure mode for a
-- destructive rollback path, rather than silently corrupting data.
ALTER TYPE notification_event RENAME TO notification_event_old;
CREATE TYPE notification_event AS ENUM ('new_post', 'new_comment', 'mention', 'github_event');
ALTER TABLE notifications
    ALTER COLUMN event TYPE notification_event
    USING event::text::notification_event;
DROP TYPE notification_event_old;
