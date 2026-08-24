package db

import (
	"testing"

	"github.com/pressly/goose/v3"
)

// versionBeforeContextTokens is the migration immediately preceding the one
// that adds sessions.context_tokens.
const (
	versionBeforeContextTokens = 20260815000017
	versionContextTokens       = 20260823000018
)

// Upgrading must not leave every existing session reading "Context 0" until its
// next turn. The occupancy is already recoverable from the call ledger, so the
// migration backfills it -- a user who reopens yesterday's session sees what it
// actually holds.
func TestContextTokensBackfillsFromTheLedger(t *testing.T) {
	conn := openAt(t, versionBeforeContextTokens)

	seedStmts := []string{
		`INSERT INTO sessions (id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at)
		 VALUES ('sess-busy', 'seven turns', 0, 147875, 395, 0, 1000, 1000)`,
		`INSERT INTO sessions (id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at)
		 VALUES ('sess-untouched', 'never ran a turn', 0, 0, 0, 0, 1000, 1000)`,
		// Two settled turns over the same conversation, plus one still in flight.
		`INSERT INTO model_calls (model_call_id, session_id, provider, model, status, started_at, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens)
		 VALUES ('call-1', 'sess-busy', 'local', 'm', 'completed', 1000, 20996, 22, 0, 0)`,
		`INSERT INTO model_calls (model_call_id, session_id, provider, model, status, started_at, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens)
		 VALUES ('call-2', 'sess-busy', 'local', 'm', 'completed', 2000, 154, 270, 0, 21248)`,
		`INSERT INTO model_calls (model_call_id, session_id, provider, model, status, started_at, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens)
		 VALUES ('call-3', 'sess-busy', 'local', 'm', 'started', 3000, 0, 0, 0, 0)`,
	}
	for _, s := range seedStmts {
		if _, err := conn.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}

	if err := goose.UpTo(conn, "migrations", versionContextTokens); err != nil {
		t.Fatalf("upgrade to %d: %v", versionContextTokens, err)
	}

	var busy int64
	if err := conn.QueryRow(`SELECT context_tokens FROM sessions WHERE id='sess-busy'`).Scan(&busy); err != nil {
		t.Fatalf("read backfilled occupancy: %v", err)
	}
	// The last settled turn only: its whole input, cached included, plus output.
	if want := int64(154 + 21248 + 270); busy != want {
		t.Errorf("backfilled context_tokens = %d, want %d", busy, want)
	}
	// And emphatically not the cumulative figure the header used to show.
	if busy >= 147875 {
		t.Errorf("backfill reproduced the cumulative sum (%d); it must be occupancy", busy)
	}

	var untouched int64
	if err := conn.QueryRow(`SELECT context_tokens FROM sessions WHERE id='sess-untouched'`).Scan(&untouched); err != nil {
		t.Fatalf("read untouched session: %v", err)
	}
	if untouched != 0 {
		t.Errorf("a session with no calls should backfill to 0, got %d", untouched)
	}
}
