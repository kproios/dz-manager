-- Provider kinds are generic: 'selfhosted' (any machine you control),
-- 'managed' (managed panel) or 'generic'. Older databases used a tool-specific
-- name for self-hosted servers; the column is rebuilt so its CHECK constraint
-- only allows the generic names. (DROP COLUMN is allowed because the CHECK
-- belongs to the column itself.)
ALTER TABLE server_profiles ADD COLUMN provider_kind_new TEXT NOT NULL DEFAULT 'selfhosted'
    CHECK (provider_kind_new IN ('selfhosted','managed','generic'));
UPDATE server_profiles SET provider_kind_new = CASE provider_kind WHEN 'managed' THEN 'managed' WHEN 'generic' THEN 'generic' ELSE 'selfhosted' END;
ALTER TABLE server_profiles DROP COLUMN provider_kind;
ALTER TABLE server_profiles RENAME COLUMN provider_kind_new TO provider_kind;
