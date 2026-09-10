ALTER TABLE deployments ADD COLUMN version INTEGER;

UPDATE deployments d
SET version = sub.rn
FROM (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY name ORDER BY created_at, id) AS rn
    FROM deployments
) sub
WHERE d.id = sub.id;

ALTER TABLE deployments ALTER COLUMN version SET NOT NULL;
ALTER TABLE deployments ADD CONSTRAINT deployments_name_version_key UNIQUE (name, version);
