CREATE TABLE IF NOT EXISTS evaluations (
    id            SERIAL PRIMARY KEY,
    deployment_id INTEGER NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    inputs        JSONB NOT NULL,
    outputs       JSONB,
    trace         JSONB,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS evaluations_deployment_id_created_at_idx
    ON evaluations (deployment_id, created_at DESC);
