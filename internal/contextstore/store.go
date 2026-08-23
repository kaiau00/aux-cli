package contextstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Store persists context pages, versions, bindings, and accesses.
type Store struct {
	db db.DBTX
}

// NewStore returns a context-page store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UpsertPage returns the page for (project, type, stableKey), creating it if absent.
func (s *Store) UpsertPage(ctx context.Context, projectID, pageType, stableKey, scope string) (Page, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT page_id, project_id, page_type, stable_key, scope, created_at
         FROM context_pages WHERE project_id IS ? AND page_type = ? AND stable_key = ?`,
		nullable(projectID), pageType, stableKey)
	var p Page
	var pid sql.NullString
	err := row.Scan(&p.ID, &pid, &p.Type, &p.StableKey, &p.Scope, &p.CreatedAt)
	if err == nil {
		p.ProjectID = pid.String
		return p, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Page{}, fmt.Errorf("failed to look up page: %w", err)
	}
	p = Page{ID: ids.New(), ProjectID: projectID, Type: pageType, StableKey: stableKey, Scope: scope, CreatedAt: time.Now().UnixMilli()}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO context_pages (page_id, project_id, page_type, stable_key, scope, created_at) VALUES (?,?,?,?,?,?)`,
		p.ID, nullable(p.ProjectID), p.Type, p.StableKey, p.Scope, p.CreatedAt); err != nil {
		return Page{}, fmt.Errorf("failed to create page: %w", err)
	}
	return p, nil
}

// UpsertVersion returns the version for (page, contentHash), creating it if absent.
func (s *Store) UpsertVersion(ctx context.Context, pageID, contentHash, sourceRevision string, tokenCount int64) (PageVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT page_version_id, page_id, content_hash, source_revision, token_count, created_at
         FROM context_page_versions WHERE page_id = ? AND content_hash = ?`, pageID, contentHash)
	var v PageVersion
	err := row.Scan(&v.ID, &v.PageID, &v.ContentHash, &v.SourceRevision, &v.TokenCount, &v.CreatedAt)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PageVersion{}, fmt.Errorf("failed to look up page version: %w", err)
	}
	v = PageVersion{ID: ids.New(), PageID: pageID, ContentHash: contentHash, SourceRevision: sourceRevision, TokenCount: tokenCount, MetadataJSON: "{}", CreatedAt: time.Now().UnixMilli()}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO context_page_versions (page_version_id, page_id, content_hash, source_revision, token_count, metadata_json, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		v.ID, v.PageID, v.ContentHash, v.SourceRevision, v.TokenCount, v.MetadataJSON, v.CreatedAt); err != nil {
		return PageVersion{}, fmt.Errorf("failed to create page version: %w", err)
	}
	return v, nil
}

// Bind records a page version in a model call's working set.
func (s *Store) Bind(ctx context.Context, b Binding) error {
	if b.ID == "" {
		b.ID = ids.New()
	}
	if b.BoundAt == 0 {
		b.BoundAt = time.Now().UnixMilli()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO context_bindings (id, task_id, model_call_id, page_version_id, state, rank, reason, token_count, bound_at)
         VALUES (?,?,?,?,?,?,?,?,?)`,
		b.ID, nullable(b.TaskID), b.ModelCallID, b.PageVersionID, b.State, b.Rank, b.Reason, b.TokenCount, b.BoundAt)
	if err != nil {
		return fmt.Errorf("failed to bind page: %w", err)
	}
	return nil
}

// RecordAccess records that a page version was accessed during a call.
func (s *Store) RecordAccess(ctx context.Context, taskID, callID, pageVersionID, accessType, usefulSignal string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO page_accesses (id, task_id, model_call_id, page_version_id, access_type, useful_signal, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		ids.New(), nullable(taskID), callID, pageVersionID, accessType, usefulSignal, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to record page access: %w", err)
	}
	return nil
}

// Exclude marks a tool call's result as excluded from a task's future prompt
// compiles: the TUI's cross-off control changes what
// is actually sent to the model on the next turn, not just how it's
// displayed. Idempotent.
func (s *Store) Exclude(ctx context.Context, taskID, toolCallID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO context_exclusions (id, task_id, tool_call_id, created_at) VALUES (?,?,?,?)`,
		ids.New(), taskID, toolCallID, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to exclude tool call: %w", err)
	}
	return nil
}

// Include clears a single previously recorded exclusion (the TUI's un-cross
// control). Not an error if none existed.
func (s *Store) Include(ctx context.Context, taskID, toolCallID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM context_exclusions WHERE task_id = ? AND tool_call_id = ?`, taskID, toolCallID)
	if err != nil {
		return fmt.Errorf("failed to include tool call: %w", err)
	}
	return nil
}

// ClearExclusions clears every exclusion for a task (the TUI's "clear
// crossed" control).
func (s *Store) ClearExclusions(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM context_exclusions WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("failed to clear exclusions: %w", err)
	}
	return nil
}

// Exclusions returns the set of tool-call ids excluded from a task's next
// prompt compile.
func (s *Store) Exclusions(ctx context.Context, taskID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool_call_id FROM context_exclusions WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// Pin marks a tool call's result as pinned:
// the prompt compiler guarantees its full content is sent on the task's next
// compile — exempt from both dedup stubbing and exclusion stubbing — rather
// than only repainting a local checkbox. Idempotent.
func (s *Store) Pin(ctx context.Context, taskID, toolCallID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO context_pins (id, task_id, tool_call_id, created_at) VALUES (?,?,?,?)`,
		ids.New(), taskID, toolCallID, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to pin tool call: %w", err)
	}
	return nil
}

// Unpin clears a single previously recorded pin. Not an error if none existed.
func (s *Store) Unpin(ctx context.Context, taskID, toolCallID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM context_pins WHERE task_id = ? AND tool_call_id = ?`, taskID, toolCallID)
	if err != nil {
		return fmt.Errorf("failed to unpin tool call: %w", err)
	}
	return nil
}

// ClearPins clears every pin for a task.
func (s *Store) ClearPins(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM context_pins WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("failed to clear pins: %w", err)
	}
	return nil
}

// Pins returns the set of tool-call ids pinned for a task's next prompt compile.
func (s *Store) Pins(ctx context.Context, taskID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool_call_id FROM context_pins WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// BindingsForCall returns the page bindings of a model call, joined with page
// identity, ordered by state then rank. This is the "explain page by page" read.
func (s *Store) BindingsForCall(ctx context.Context, callID string) ([]BoundPage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT b.id, b.task_id, b.model_call_id, b.page_version_id, b.state, b.rank, b.reason, b.token_count, b.bound_at,
                p.page_type, p.stable_key
         FROM context_bindings b
         JOIN context_page_versions v ON v.page_version_id = b.page_version_id
         JOIN context_pages p ON p.page_id = v.page_id
         WHERE b.model_call_id = ?
         ORDER BY b.state, b.rank`, callID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BoundPage
	for rows.Next() {
		var bp BoundPage
		var taskID sql.NullString
		if err := rows.Scan(&bp.ID, &taskID, &bp.ModelCallID, &bp.PageVersionID, &bp.State, &bp.Rank, &bp.Reason,
			&bp.TokenCount, &bp.BoundAt, &bp.PageType, &bp.StableKey); err != nil {
			return nil, err
		}
		bp.TaskID = taskID.String
		out = append(out, bp)
	}
	return out, rows.Err()
}
