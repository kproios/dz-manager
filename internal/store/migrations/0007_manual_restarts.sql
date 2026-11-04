-- Preserve observed history while allowing explicit panel-requested restarts.
ALTER TABLE restart_events RENAME TO restart_events_previous;
CREATE TABLE restart_events (
    id INTEGER PRIMARY KEY,
    server_id TEXT NOT NULL,
    down_at TEXT,
    up_at TEXT,
    planned_for TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('planned','unplanned','manual','missed')),
    outcome TEXT NOT NULL CHECK (outcome IN ('down','ok','not_back','recovered_late','missed')),
    duration_s INTEGER
);
INSERT INTO restart_events SELECT * FROM restart_events_previous;
DROP TABLE restart_events_previous;
CREATE INDEX restart_events_server ON restart_events(server_id, down_at);
