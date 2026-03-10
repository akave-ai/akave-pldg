-- log_batches: metadata index for O3 chunk objects.
-- One row per chunk flushed to O3; enables fast query-time lookup without scanning O3 directly.
CREATE TABLE IF NOT EXISTS log_batches (
    id             UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id     UUID        REFERENCES projects(id) ON DELETE SET NULL,
    tenant         TEXT        NOT NULL DEFAULT 'default',
    stream_id      TEXT        NOT NULL,
    service        TEXT        NOT NULL DEFAULT '',
    ts_start       TIMESTAMPTZ NOT NULL,
    ts_end         TIMESTAMPTZ NOT NULL,
    levels         TEXT[]      NOT NULL DEFAULT '{}',
    tags           JSONB       NOT NULL DEFAULT '{}',
    o3_object_key  TEXT        NOT NULL,
    entry_count    INT         NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for the primary query patterns:
--   * time-range scans (most common)
--   * tenant isolation
--   * stream lookup (for index reader parity)
--   * service filter
CREATE INDEX IF NOT EXISTS idx_log_batches_tenant_ts
    ON log_batches (tenant, ts_start, ts_end);

CREATE INDEX IF NOT EXISTS idx_log_batches_project_ts
    ON log_batches (project_id, ts_start, ts_end);

CREATE INDEX IF NOT EXISTS idx_log_batches_stream_id
    ON log_batches (stream_id);

CREATE INDEX IF NOT EXISTS idx_log_batches_service
    ON log_batches (service);

CREATE INDEX IF NOT EXISTS idx_log_batches_levels
    ON log_batches USING gin (levels);

CREATE INDEX IF NOT EXISTS idx_log_batches_tags
    ON log_batches USING gin (tags);

-- alert_rules: user-defined alert rules evaluated by the background worker.
CREATE TABLE IF NOT EXISTS alert_rules (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id  UUID        REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    type        TEXT        NOT NULL CHECK (type IN ('keyword', 'threshold')),
    conditions  JSONB       NOT NULL DEFAULT '{}',
    actions     JSONB       NOT NULL DEFAULT '{}',
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_rules_project_id
    ON alert_rules (project_id);

CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled
    ON alert_rules (enabled) WHERE enabled = TRUE;

CREATE TRIGGER set_alert_rules_updated_at
    BEFORE UPDATE ON alert_rules
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- alert_events: history of triggered alerts (one row per rule evaluation that fired).
CREATE TABLE IF NOT EXISTS alert_events (
    id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    rule_id      UUID        NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    match_count  INT         NOT NULL DEFAULT 0,
    details      JSONB       NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_alert_events_rule_id
    ON alert_events (rule_id, triggered_at DESC);

---- create above / drop below ----

DROP TABLE IF EXISTS alert_events;
DROP TABLE IF EXISTS alert_rules;
DROP TABLE IF EXISTS log_batches;