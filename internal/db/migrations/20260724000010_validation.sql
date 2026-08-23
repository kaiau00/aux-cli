-- +goose Up
-- +goose StatementBegin
-- Validation runs and acceptance evidence. Checkpoint tables land separately.

CREATE TABLE IF NOT EXISTS validation_runs (
    validation_run_id TEXT PRIMARY KEY,
    task_id TEXT,
    intent_id TEXT NOT NULL DEFAULT '',
    validator_type TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL DEFAULT '',
    command_hash TEXT NOT NULL DEFAULT '',
    input_fingerprint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',  -- pending|running|passed|failed|skipped|blocked
    started_at INTEGER NOT NULL DEFAULT 0,
    finished_at INTEGER,
    exit_code INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    output_artifact_id TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_validation_runs_task ON validation_runs (task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_validation_runs_cache ON validation_runs (command_hash, input_fingerprint, status);

CREATE TABLE IF NOT EXISTS validation_evidence (
    id TEXT PRIMARY KEY,
    task_id TEXT,
    criterion_id TEXT NOT NULL DEFAULT '',
    validation_run_id TEXT NOT NULL DEFAULT '',
    evidence_type TEXT NOT NULL DEFAULT '',  -- executable|inspection|diff|user_waiver
    summary TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_validation_evidence_task ON validation_evidence (task_id);
CREATE INDEX IF NOT EXISTS idx_validation_evidence_criterion ON validation_evidence (task_id, criterion_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS validation_evidence;
DROP TABLE IF EXISTS validation_runs;
-- +goose StatementEnd
