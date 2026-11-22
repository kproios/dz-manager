-- The managed-panel provider kind is now called 'managed'. Databases created
-- by older releases stored it under a host-specific name; every value that is
-- not 'selfhosted' or 'generic' becomes 'managed', and the CHECK constraint is
-- rebuilt to allow only the three generic names. On a new database this is a
-- no-op rebuild.
ALTER TABLE server_profiles ADD COLUMN provider_kind_new TEXT NOT NULL DEFAULT 'selfhosted'
    CHECK (provider_kind_new IN ('selfhosted','managed','generic'));
UPDATE server_profiles SET provider_kind_new = CASE provider_kind WHEN 'selfhosted' THEN 'selfhosted' WHEN 'generic' THEN 'generic' ELSE 'managed' END;
ALTER TABLE server_profiles DROP COLUMN provider_kind;
ALTER TABLE server_profiles RENAME COLUMN provider_kind_new TO provider_kind;
