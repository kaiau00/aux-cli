package cost

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
)

// Service persists and reads the per-call model ledger.
//
// Reads for session/task totals are computed with SQL aggregation so they always
// reconcile with the underlying rows ("Token and cost totals reconcile from call to task to session").
type Service interface {
	// StartCall inserts a call in the `started` state and returns it.
	StartCall(ctx context.Context, call ModelCall) (ModelCall, error)
	// FinishCall updates a started call to a terminal state with final usage,
	// timing, and cost.
	FinishCall(ctx context.Context, call ModelCall) error
	// GetCall returns a single call by id.
	GetCall(ctx context.Context, id string) (ModelCall, error)
	// ListCallsBySession returns calls for a session ordered by start time.
	ListCallsBySession(ctx context.Context, sessionID string) ([]ModelCall, error)
	// ListCallsByTask returns calls for a task ordered by start time.
	ListCallsByTask(ctx context.Context, taskID string) ([]ModelCall, error)
	// SessionTotals aggregates completed/failed calls for a session.
	SessionTotals(ctx context.Context, sessionID string) (Totals, error)
	// TaskTotals aggregates completed/failed calls for a task.
	TaskTotals(ctx context.Context, taskID string) (Totals, error)
	// SessionContextTokens returns how many tokens the session's most recent
	// completed call actually occupied in the model's context window. Unlike
	// the Totals figures, which sum every call and answer "what did this
	// cost", this answers "how full is the window right now".
	SessionContextTokens(ctx context.Context, sessionID string) (int64, error)
}

type service struct {
	db db.DBTX
}

// NewService returns a ledger backed by the given database handle.
func NewService(dbtx db.DBTX) Service {
	return &service{db: dbtx}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *service) StartCall(ctx context.Context, call ModelCall) (ModelCall, error) {
	if call.Status == "" {
		call.Status = CallStarted
	}
	if call.PriceCatalogVersion == "" {
		call.PriceCatalogVersion = PriceCatalogVersion
	}
	if call.CostState == "" {
		call.CostState = CostKnown
	}
	const q = `
INSERT INTO model_calls (
    model_call_id, project_id, task_id, turn_id, session_id, message_id,
    provider, model, status, retry_group, price_catalog_version, cost_state,
    started_at, finished_at, first_token_at, latency_ms, ttft_ms,
    input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
    estimated_cost, error_code
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		call.ID, nullable(call.ProjectID), nullable(call.TaskID), nullable(call.TurnID),
		nullable(call.SessionID), nullable(call.MessageID),
		call.Provider, call.Model, string(call.Status), nullable(call.RetryGroup),
		call.PriceCatalogVersion, string(call.CostState),
		call.StartedAt, nullOrInt(call.FinishedAt), nullOrInt(call.FirstTokenAt),
		call.LatencyMS, call.TTFTMS,
		call.InputTokens, call.OutputTokens, call.CacheCreationTokens, call.CacheReadTokens,
		call.EstimatedCost, call.ErrorCode,
	)
	if err != nil {
		return ModelCall{}, fmt.Errorf("failed to insert model call: %w", err)
	}
	return call, nil
}

func (s *service) FinishCall(ctx context.Context, call ModelCall) error {
	if call.PriceCatalogVersion == "" {
		call.PriceCatalogVersion = PriceCatalogVersion
	}
	const q = `
UPDATE model_calls SET
    status = ?, cost_state = ?, price_catalog_version = ?,
    finished_at = ?, first_token_at = ?, latency_ms = ?, ttft_ms = ?,
    input_tokens = ?, output_tokens = ?, cache_creation_tokens = ?, cache_read_tokens = ?,
    estimated_cost = ?, error_code = ?
WHERE model_call_id = ?`
	res, err := s.db.ExecContext(ctx, q,
		string(call.Status), string(call.CostState), call.PriceCatalogVersion,
		nullOrInt(call.FinishedAt), nullOrInt(call.FirstTokenAt), call.LatencyMS, call.TTFTMS,
		call.InputTokens, call.OutputTokens, call.CacheCreationTokens, call.CacheReadTokens,
		call.EstimatedCost, call.ErrorCode,
		call.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to finish model call: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("model call %s not found", call.ID)
	}
	return nil
}

func (s *service) GetCall(ctx context.Context, id string) (ModelCall, error) {
	const q = `SELECT ` + callColumns + ` FROM model_calls WHERE model_call_id = ?`
	row := s.db.QueryRowContext(ctx, q, id)
	return scanCall(row)
}

func (s *service) ListCallsBySession(ctx context.Context, sessionID string) ([]ModelCall, error) {
	return s.listCalls(ctx, `WHERE session_id = ? ORDER BY started_at ASC, model_call_id ASC`, sessionID)
}

func (s *service) ListCallsByTask(ctx context.Context, taskID string) ([]ModelCall, error) {
	return s.listCalls(ctx, `WHERE task_id = ? ORDER BY started_at ASC, model_call_id ASC`, taskID)
}

func (s *service) listCalls(ctx context.Context, where string, args ...any) ([]ModelCall, error) {
	q := `SELECT ` + callColumns + ` FROM model_calls ` + where
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list model calls: %w", err)
	}
	defer rows.Close()
	var calls []ModelCall
	for rows.Next() {
		call, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

func (s *service) SessionTotals(ctx context.Context, sessionID string) (Totals, error) {
	t, err := s.totals(ctx, "session_id", sessionID)
	if err != nil {
		return Totals{}, err
	}
	// Roll up direct child (subagent) session costs. Each child session's stored
	// cost is itself reconciled from its own ledger, so summing direct children
	// yields the full recursive spend. This preserves the historical
	// "parent session cost includes subagent spend" behaviour idempotently.
	// Reading the sessions table here is a deliberate, documented boundary bend
	// (the ledger is the natural owner of "how much did this session cost").
	row := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost),0) FROM sessions WHERE parent_session_id = ?`, sessionID)
	var childCost float64
	if err := row.Scan(&childCost); err != nil {
		return Totals{}, fmt.Errorf("failed to sum child session costs: %w", err)
	}
	t.Cost += childCost
	return t, nil
}

// SessionContextTokens reports the occupancy of the session's latest completed
// call: its entire input -- fresh, cache-creation and cache-read alike, since
// cached tokens occupy the window exactly as uncached ones do -- plus the
// output that was appended to the transcript.
//
// Summing every call instead, as the session's prompt/completion totals do,
// counts the same resident conversation once per turn: seven turns over a 21K
// conversation sum to 148K while never exceeding 21K resident. That is correct
// for spend and wrong for occupancy, and the difference grows with the session.
//
// A session with no completed call yet returns 0.
func (s *service) SessionContextTokens(ctx context.Context, sessionID string) (int64, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(input_tokens,0) + COALESCE(cache_creation_tokens,0)
     + COALESCE(cache_read_tokens,0) + COALESCE(output_tokens,0)
FROM model_calls
WHERE session_id = ? AND status != 'started'
ORDER BY started_at DESC, model_call_id DESC
LIMIT 1`, sessionID)
	var tokens int64
	switch err := row.Scan(&tokens); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("failed to read session context tokens: %w", err)
	}
	return tokens, nil
}

// TaskTotals aggregates a task's own calls plus every descendant task's calls
// (subagent tasks, and multi-repo children),
// walking tasks.parent_task_id recursively. Unlike SessionTotals' shallow sum
// of a precomputed session.cost column, this aggregates directly from
// model_calls at every level, so it stays correct however deep the subagent
// tree gets without depending on any intermediate cached total.
func (s *service) TaskTotals(ctx context.Context, taskID string) (Totals, error) {
	const q = `
WITH RECURSIVE descendant_tasks(task_id) AS (
    SELECT ?
    UNION ALL
    SELECT t.task_id FROM tasks t JOIN descendant_tasks d ON t.parent_task_id = d.task_id
)
SELECT
    COUNT(*),
    COALESCE(SUM(input_tokens),0),
    COALESCE(SUM(output_tokens),0),
    COALESCE(SUM(cache_creation_tokens),0),
    COALESCE(SUM(cache_read_tokens),0),
    COALESCE(SUM(estimated_cost),0),
    COALESCE(SUM(CASE WHEN cost_state = 'cost_unknown' THEN 1 ELSE 0 END),0)
FROM model_calls
WHERE task_id IN (SELECT task_id FROM descendant_tasks) AND status != 'started'`
	row := s.db.QueryRowContext(ctx, q, taskID)
	var t Totals
	var unknownCount int64
	if err := row.Scan(&t.Calls, &t.InputTokens, &t.OutputTokens,
		&t.CacheCreationTokens, &t.CacheReadTokens, &t.Cost, &unknownCount); err != nil {
		return Totals{}, fmt.Errorf("failed to aggregate task totals: %w", err)
	}
	t.PromptTokens = t.InputTokens + t.CacheCreationTokens
	t.CompletionTokens = t.OutputTokens + t.CacheReadTokens
	t.CostUnknown = unknownCount > 0
	return t, nil
}

// totals aggregates all non-started calls (completed, failed, cancelled) so that
// usage reported by the provider for cancelled/failed calls is still counted.
func (s *service) totals(ctx context.Context, column, value string) (Totals, error) {
	q := fmt.Sprintf(`
SELECT
    COUNT(*),
    COALESCE(SUM(input_tokens),0),
    COALESCE(SUM(output_tokens),0),
    COALESCE(SUM(cache_creation_tokens),0),
    COALESCE(SUM(cache_read_tokens),0),
    COALESCE(SUM(estimated_cost),0),
    COALESCE(SUM(CASE WHEN cost_state = 'cost_unknown' THEN 1 ELSE 0 END),0)
FROM model_calls
WHERE %s = ? AND status != 'started'`, column)
	row := s.db.QueryRowContext(ctx, q, value)
	var t Totals
	var unknownCount int64
	if err := row.Scan(&t.Calls, &t.InputTokens, &t.OutputTokens,
		&t.CacheCreationTokens, &t.CacheReadTokens, &t.Cost, &unknownCount); err != nil {
		return Totals{}, fmt.Errorf("failed to aggregate totals: %w", err)
	}
	t.PromptTokens = t.InputTokens + t.CacheCreationTokens
	t.CompletionTokens = t.OutputTokens + t.CacheReadTokens
	t.CostUnknown = unknownCount > 0
	return t, nil
}

const callColumns = `model_call_id, project_id, task_id, turn_id, session_id, message_id,
    provider, model, status, retry_group, price_catalog_version, cost_state,
    started_at, finished_at, first_token_at, latency_ms, ttft_ms,
    input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
    estimated_cost, error_code`

type scanner interface {
	Scan(dest ...any) error
}

func scanCall(row scanner) (ModelCall, error) {
	var c ModelCall
	var projectID, taskID, turnID, sessionID, messageID, retryGroup sql.NullString
	var finishedAt, firstTokenAt sql.NullInt64
	var status, costState string
	if err := row.Scan(
		&c.ID, &projectID, &taskID, &turnID, &sessionID, &messageID,
		&c.Provider, &c.Model, &status, &retryGroup, &c.PriceCatalogVersion, &costState,
		&c.StartedAt, &finishedAt, &firstTokenAt, &c.LatencyMS, &c.TTFTMS,
		&c.InputTokens, &c.OutputTokens, &c.CacheCreationTokens, &c.CacheReadTokens,
		&c.EstimatedCost, &c.ErrorCode,
	); err != nil {
		return ModelCall{}, err
	}
	c.ProjectID = projectID.String
	c.TaskID = taskID.String
	c.TurnID = turnID.String
	c.SessionID = sessionID.String
	c.MessageID = messageID.String
	c.RetryGroup = retryGroup.String
	c.Status = CallStatus(status)
	c.CostState = CostState(costState)
	c.FinishedAt = finishedAt.Int64
	c.FirstTokenAt = firstTokenAt.Int64
	return c, nil
}

func nullOrInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
