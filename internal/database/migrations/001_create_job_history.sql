CREATE TABLE IF NOT EXISTS job_history (
    id          TEXT        PRIMARY KEY,
    url         TEXT        NOT NULL,
    status      TEXT        NOT NULL,
    status_code INTEGER     NOT NULL DEFAULT 0,
    error       TEXT        NOT NULL DEFAULT '',
    duration_ms BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS job_history_finished_at_idx ON job_history (finished_at DESC);
