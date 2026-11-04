-- #4 restart observation.
CREATE TABLE restart_events (
    id          INTEGER PRIMARY KEY,
    server_id   TEXT NOT NULL,
    down_at     TEXT,            -- UTC RFC3339
    up_at       TEXT,
    planned_for TEXT,
    kind        TEXT NOT NULL CHECK (kind IN ('planned','unplanned','missed')),
    outcome     TEXT NOT NULL CHECK (outcome IN ('down','ok','not_back','recovered_late','missed')),
    duration_s  INTEGER
);
CREATE INDEX restart_events_server ON restart_events(server_id, down_at);
