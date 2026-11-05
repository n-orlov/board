-- +goose Up
CREATE TABLE board_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE board_meta;
