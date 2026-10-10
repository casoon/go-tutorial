CREATE TABLE targets (
    id               INTEGER PRIMARY KEY,
    url              TEXT    NOT NULL UNIQUE,
    interval_seconds INTEGER NOT NULL,
    created_at       INTEGER NOT NULL, -- unix milliseconds, UTC
    next_check_at    INTEGER NOT NULL  -- unix milliseconds, UTC
);

CREATE INDEX targets_next_check_at ON targets (next_check_at);

CREATE TABLE results (
    id          INTEGER PRIMARY KEY,
    target_id   INTEGER NOT NULL REFERENCES targets (id) ON DELETE CASCADE,
    status_code INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    error       TEXT    NOT NULL DEFAULT '',
    checked_at  INTEGER NOT NULL
);

CREATE INDEX results_target_checked ON results (target_id, checked_at DESC);
