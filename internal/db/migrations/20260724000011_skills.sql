-- +goose Up
-- +goose StatementBegin
-- Skills, policies, experiments, and evaluation. Skills and policies are
-- promoted only with evaluation/replay evidence.
-- Skill content is stored inline as JSON here (content_artifact_id remains for
-- large bodies) so the skill lifecycle is testable without the artifact store.

CREATE TABLE IF NOT EXISTS skills (
    skill_id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'candidate', -- candidate|active|rolled_back|rejected|archived
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (owner_type, owner_id, name)
);

CREATE TABLE IF NOT EXISTS skill_versions (
    skill_version_id TEXT PRIMARY KEY,
    skill_id TEXT NOT NULL,
    content_json TEXT NOT NULL DEFAULT '{}',
    content_artifact_id TEXT,
    source_type TEXT NOT NULL DEFAULT '',
    source_ids_json TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (skill_id) REFERENCES skills (skill_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_skill_versions_skill ON skill_versions (skill_id, created_at);

CREATE TABLE IF NOT EXISTS skill_evaluations (
    id TEXT PRIMARY KEY,
    skill_version_id TEXT NOT NULL,
    eval_run_id TEXT NOT NULL DEFAULT '',
    baseline_version_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',  -- pass|fail|inconclusive
    metrics_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (skill_version_id) REFERENCES skill_versions (skill_version_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_skill_evaluations_version ON skill_evaluations (skill_version_id);

CREATE TABLE IF NOT EXISTS governor_policies (
    policy_id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    task_class TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'candidate',
    policy_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS experiments (
    experiment_id TEXT PRIMARY KEY,
    project_id TEXT,
    name TEXT NOT NULL,
    hypothesis TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    config_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS eval_cases (
    eval_case_id TEXT PRIMARY KEY,
    project_id TEXT,
    name TEXT NOT NULL,
    fixture_artifact_id TEXT,
    expected_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS eval_runs (
    eval_run_id TEXT PRIMARY KEY,
    experiment_id TEXT NOT NULL DEFAULT '',
    eval_case_id TEXT NOT NULL DEFAULT '',
    variant TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    started_at INTEGER NOT NULL DEFAULT 0,
    finished_at INTEGER,
    metrics_json TEXT NOT NULL DEFAULT '{}',
    replay_artifact_id TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_eval_runs_experiment ON eval_runs (experiment_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS eval_runs;
DROP TABLE IF EXISTS eval_cases;
DROP TABLE IF EXISTS experiments;
DROP TABLE IF EXISTS governor_policies;
DROP TABLE IF EXISTS skill_evaluations;
DROP TABLE IF EXISTS skill_versions;
DROP TABLE IF EXISTS skills;
-- +goose StatementEnd
