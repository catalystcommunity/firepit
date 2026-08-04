-- +goose Up
-- Categories classify posts within a board. A category can be assigned to
-- more than one board. If cross_board_posting is true, a post that has the
-- category is visible in each assigned board. Posts with no post_categories
-- rows are the virtual Uncategorized category.
ALTER TABLE boards
    ADD COLUMN category_limit integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT boards_category_limit_check CHECK (category_limit BETWEEN 0 AND 100);

CREATE TABLE categories (
    id                  uuid PRIMARY KEY DEFAULT generate_ulid(),
    slug                text NOT NULL UNIQUE,
    name                text NOT NULL,
    description         text NOT NULL DEFAULT '',
    cross_board_posting boolean NOT NULL DEFAULT false,
    created_by          uuid NOT NULL REFERENCES users,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT categories_slug_check CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT categories_name_check CHECK (char_length(name) BETWEEN 1 AND 80)
);

CREATE TABLE board_categories (
    board_id    uuid NOT NULL REFERENCES boards ON DELETE CASCADE,
    category_id uuid NOT NULL REFERENCES categories ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (board_id, category_id)
);
CREATE INDEX board_categories_category_idx ON board_categories (category_id, board_id);

CREATE TABLE post_categories (
    post_id     uuid NOT NULL REFERENCES posts ON DELETE CASCADE,
    category_id uuid NOT NULL REFERENCES categories ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, category_id)
);
CREATE INDEX post_categories_category_idx ON post_categories (category_id, post_id);

-- Supports the virtual Uncategorized filter without scanning every category
-- relation for deleted posts.
CREATE INDEX post_categories_post_idx ON post_categories (post_id);

-- +goose Down
DROP TABLE post_categories;
DROP TABLE board_categories;
DROP TABLE categories;
ALTER TABLE boards DROP CONSTRAINT boards_category_limit_check;
ALTER TABLE boards DROP COLUMN category_limit;
