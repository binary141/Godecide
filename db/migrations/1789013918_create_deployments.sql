CREATE TABLE IF NOT EXISTS deployments (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    namespace   TEXT NOT NULL,
    dmn_version TEXT NOT NULL,
    xml         TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
