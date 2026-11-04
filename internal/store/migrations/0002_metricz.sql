-- #5 MetricZ server performance, 1-minute buckets, 30-day retention.
CREATE TABLE metricz_perf (
    server_id TEXT NOT NULL,
    minute    TEXT NOT NULL,   -- UTC "YYYY-MM-DDTHH:MM"
    fps       REAL,
    fps_min   REAL,
    players   REAL,
    infected  REAL,
    animals   REAL,
    items     REAL,
    vehicles  REAL,
    PRIMARY KEY (server_id, minute)
);
