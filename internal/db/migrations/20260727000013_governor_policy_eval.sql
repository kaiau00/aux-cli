-- +goose Up
-- +goose StatementBegin
-- Governor policy evaluations. A learned governor
-- policy is promoted from candidate to active only with a passing evaluation
-- against a baseline — never autonomously. This mirrors the skill evaluation
-- gate so both share the same evidence-before-default discipline.
CREATE TABLE IF NOT EXISTS governor_policy_evaluations (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL,
    baseline_policy_id TEXT NOT NULL DEFAULT '',
    eval_run_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '', -- pass|fail|inconclusive
    metrics_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (policy_id) REFERENCES governor_policies (policy_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_governor_policy_evaluations_policy
    ON governor_policy_evaluations (policy_id, result);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS governor_policy_evaluations;
-- +goose StatementEnd
