// Package toolexec persists tool-execution observability records and adapts them
// to the tools.Recorder interface used by tools.Executor.
// and Migration A (tool_executions).
package toolexec

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

// Store reads and writes the tool_executions table.
type Store struct {
	db db.DBTX
}

// NewStore returns a tool-execution store backed by the given database handle.
func NewStore(dbtx db.DBTX) *Store {
	return &Store{db: dbtx}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Insert records a started tool execution.
func (s *Store) Insert(ctx context.Context, rec tools.ExecutionRecord) error {
	const q = `
INSERT INTO tool_executions (
    tool_execution_id, project_id, task_id, turn_id, session_id, model_call_id,
    tool_call_id, tool_name, input_hash, status, started_at, metadata_json
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		rec.ID, nullable(rec.Correlation.ProjectID), nullable(rec.Correlation.TaskID),
		nullable(rec.Correlation.TurnID), nullable(rec.Correlation.SessionID),
		nullable(rec.Correlation.ModelCallID), nullable(rec.ToolCallID),
		rec.ToolName, rec.InputHash, rec.Status, rec.StartedAt, metadataOrDefault(rec.Metadata),
	)
	if err != nil {
		return fmt.Errorf("failed to insert tool execution: %w", err)
	}
	return nil
}

// Finish updates a tool execution to its terminal state.
func (s *Store) Finish(ctx context.Context, rec tools.ExecutionRecord) error {
	const q = `
UPDATE tool_executions SET
    status = ?, finished_at = ?, latency_ms = ?, response_bytes = ?,
    is_error = ?, artifact_id = ?, metadata_json = ?
WHERE tool_execution_id = ?`
	_, err := s.db.ExecContext(ctx, q,
		rec.Status, rec.FinishedAt, rec.LatencyMS, rec.ResponseBytes,
		boolToInt(rec.IsError), nullable(rec.ArtifactID), metadataOrDefault(rec.Metadata),
		rec.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to finish tool execution: %w", err)
	}
	return nil
}

// Get returns a tool execution by id.
func (s *Store) Get(ctx context.Context, id string) (tools.ExecutionRecord, error) {
	const q = `SELECT ` + columns + ` FROM tool_executions WHERE tool_execution_id = ?`
	return scan(s.db.QueryRowContext(ctx, q, id))
}

// ListByTask returns tool executions for a task ordered by start time.
func (s *Store) ListByTask(ctx context.Context, taskID string) ([]tools.ExecutionRecord, error) {
	return s.list(ctx, `WHERE task_id = ? ORDER BY started_at ASC`, taskID)
}

// ListBySession returns tool executions for a session ordered by start time.
func (s *Store) ListBySession(ctx context.Context, sessionID string) ([]tools.ExecutionRecord, error) {
	return s.list(ctx, `WHERE session_id = ? ORDER BY started_at ASC`, sessionID)
}

func (s *Store) list(ctx context.Context, where string, args ...any) ([]tools.ExecutionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM tool_executions `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tool executions: %w", err)
	}
	defer rows.Close()
	var recs []tools.ExecutionRecord
	for rows.Next() {
		rec, err := scan(rows)
		if err != nil {
			return nil, err
		}
		recs = append(recs, rec)
	}
	return recs, rows.Err()
}

const columns = `tool_execution_id, project_id, task_id, turn_id, session_id, model_call_id,
    tool_call_id, tool_name, input_hash, status, started_at, finished_at, latency_ms,
    response_bytes, is_error, artifact_id, metadata_json`

type scanner interface {
	Scan(dest ...any) error
}

func scan(row scanner) (tools.ExecutionRecord, error) {
	var r tools.ExecutionRecord
	var projectID, taskID, turnID, sessionID, modelCallID, toolCallID, artifactID sql.NullString
	var finishedAt sql.NullInt64
	var isError int64
	if err := row.Scan(
		&r.ID, &projectID, &taskID, &turnID, &sessionID, &modelCallID,
		&toolCallID, &r.ToolName, &r.InputHash, &r.Status, &r.StartedAt, &finishedAt, &r.LatencyMS,
		&r.ResponseBytes, &isError, &artifactID, &r.Metadata,
	); err != nil {
		return tools.ExecutionRecord{}, err
	}
	r.Correlation = tools.Correlation{
		ProjectID:   projectID.String,
		TaskID:      taskID.String,
		TurnID:      turnID.String,
		SessionID:   sessionID.String,
		ModelCallID: modelCallID.String,
	}
	r.ToolCallID = toolCallID.String
	r.ArtifactID = artifactID.String
	r.FinishedAt = finishedAt.Int64
	r.IsError = isError != 0
	return r, nil
}

func metadataOrDefault(m string) string {
	if m == "" {
		return "{}"
	}
	return m
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
