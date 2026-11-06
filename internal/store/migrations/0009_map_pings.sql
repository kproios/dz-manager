-- When the panel last received a ping for a marker. Measured on the panel's own
-- clock, so a wrong game-server clock can never keep a ghost marker alive.
ALTER TABLE map_events ADD COLUMN pinged_at TEXT;
