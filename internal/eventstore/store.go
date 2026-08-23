package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/pubsub"
)

// Service appends and reads durable domain events and notifies subscribers after
// commit.
type Service interface {
	pubsub.Suscriber[Event]

	// Append records an event, assigning id/sequence/timestamp, then publishes an
	// in-process notification after the row commits.
	Append(ctx context.Context, in Append) (Event, error)
	// List returns events matching the filter in ascending sequence order.
	List(ctx context.Context, f Filter) ([]Event, error)
	// LatestSequence returns the highest assigned sequence (0 if empty).
	LatestSequence(ctx context.Context) (int64, error)

	// SaveCheckpoint records a projection's last applied sequence.
	SaveCheckpoint(ctx context.Context, projection string, sequence int64) error
	// LoadCheckpoint returns a projection's last applied sequence (0 if none).
	LoadCheckpoint(ctx context.Context, projection string) (int64, error)
}

type service struct {
	*pubsub.Broker[Event]
	db db.DBTX
}

// NewService returns an event store backed by the given database handle.
func NewService(dbtx db.DBTX) Service {
	return &service{
		Broker: pubsub.NewBroker[Event](),
		db:     dbtx,
	}
}

func (s *service) Append(ctx context.Context, in Append) (Event, error) {
	payload := json.RawMessage("{}")
	if in.Payload != nil {
		b, err := json.Marshal(in.Payload)
		if err != nil {
			return Event{}, fmt.Errorf("failed to marshal event payload: %w", err)
		}
		payload = b
	}
	occurredAt := in.OccurredAt
	if occurredAt == 0 {
		occurredAt = time.Now().UnixMilli()
	}
	ev := Event{
		ID:            ids.New(),
		Type:          in.Type,
		SchemaVersion: SchemaVersion,
		ProjectID:     in.ProjectID,
		SessionID:     in.SessionID,
		TaskID:        in.TaskID,
		TurnID:        in.TurnID,
		OccurredAt:    occurredAt,
		Payload:       payload,
	}

	// Assign the monotonic sequence atomically inside the insert and read it
	// back. Under SQLite's single writer this MAX+1 is race-free (ADR 0002).
	const q = `
INSERT INTO domain_events (
    event_id, sequence, event_type, schema_version,
    project_id, session_id, task_id, turn_id, occurred_at, payload_json
) VALUES (
    ?, (SELECT COALESCE(MAX(sequence),0)+1 FROM domain_events), ?, ?, ?, ?, ?, ?, ?, ?
) RETURNING sequence`
	row := s.db.QueryRowContext(ctx, q,
		ev.ID, string(ev.Type), ev.SchemaVersion,
		nullable(ev.ProjectID), nullable(ev.SessionID), nullable(ev.TaskID), nullable(ev.TurnID),
		ev.OccurredAt, string(ev.Payload),
	)
	if err := row.Scan(&ev.Sequence); err != nil {
		return Event{}, fmt.Errorf("failed to append event: %w", err)
	}

	// Publish only after the row is committed (autocommit has completed here).
	s.Publish(pubsub.CreatedEvent, ev)
	return ev, nil
}

func (s *service) List(ctx context.Context, f Filter) ([]Event, error) {
	var where []string
	var args []any
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, f.SessionID)
	}
	if f.TaskID != "" {
		where = append(where, "task_id = ?")
		args = append(args, f.TaskID)
	}
	if f.TurnID != "" {
		where = append(where, "turn_id = ?")
		args = append(args, f.TurnID)
	}
	if f.AfterSequence > 0 {
		where = append(where, "sequence > ?")
		args = append(args, f.AfterSequence)
	}
	if len(f.Types) > 0 {
		placeholders := make([]string, len(f.Types))
		for i, t := range f.Types {
			placeholders[i] = "?"
			args = append(args, string(t))
		}
		where = append(where, "event_type IN ("+strings.Join(placeholders, ",")+")")
	}

	q := `SELECT event_id, sequence, event_type, schema_version,
        project_id, session_id, task_id, turn_id, occurred_at, payload_json
        FROM domain_events`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY sequence ASC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func (s *service) LatestSequence(ctx context.Context) (int64, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM domain_events`)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		return 0, fmt.Errorf("failed to read latest sequence: %w", err)
	}
	return seq, nil
}

func (s *service) SaveCheckpoint(ctx context.Context, projection string, sequence int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO projection_checkpoints (projection_name, last_sequence, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(projection_name) DO UPDATE SET last_sequence = excluded.last_sequence, updated_at = excluded.updated_at`,
		projection, sequence, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}
	return nil
}

func (s *service) LoadCheckpoint(ctx context.Context, projection string) (int64, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT last_sequence FROM projection_checkpoints WHERE projection_name = ?`, projection)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to load checkpoint: %w", err)
	}
	return seq, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (Event, error) {
	var ev Event
	var projectID, sessionID, taskID, turnID sql.NullString
	var payload string
	var eventType string
	if err := row.Scan(
		&ev.ID, &ev.Sequence, &eventType, &ev.SchemaVersion,
		&projectID, &sessionID, &taskID, &turnID, &ev.OccurredAt, &payload,
	); err != nil {
		return Event{}, err
	}
	ev.Type = Type(eventType)
	ev.ProjectID = projectID.String
	ev.SessionID = sessionID.String
	ev.TaskID = taskID.String
	ev.TurnID = turnID.String
	ev.Payload = json.RawMessage(payload)
	return ev, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
