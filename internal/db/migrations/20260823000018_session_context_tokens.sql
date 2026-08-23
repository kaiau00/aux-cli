-- +goose Up
-- +goose StatementBegin
-- Context occupancy, as distinct from cumulative spend.
--
-- prompt_tokens/completion_tokens are lifetime sums over every model call in
-- the session, which is the right basis for cost but not for "how full is the
-- window". A seven-turn session that resends the same 21K conversation each
-- turn sums to 148K while only ~21K is ever resident, so the header read 7x
-- high and auto-compaction fired that much too early.
--
-- context_tokens holds the most recent completed call's occupancy instead:
-- its whole input (fresh, cache-creation and cache-read) plus its output.
-- Additive and defaulted, so existing rows read zero until their next call.
ALTER TABLE sessions ADD COLUMN context_tokens INTEGER NOT NULL DEFAULT 0
    CHECK (context_tokens >= 0);
-- +goose StatementEnd

-- +goose StatementBegin
-- Backfill from the ledger so an existing session shows its real occupancy the
-- moment it is reopened, rather than reading zero until its next turn.
UPDATE sessions SET context_tokens = COALESCE((
    SELECT COALESCE(mc.input_tokens, 0) + COALESCE(mc.cache_creation_tokens, 0)
         + COALESCE(mc.cache_read_tokens, 0) + COALESCE(mc.output_tokens, 0)
    FROM model_calls mc
    WHERE mc.session_id = sessions.id AND mc.status != 'started'
    ORDER BY mc.started_at DESC, mc.model_call_id DESC
    LIMIT 1
), 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN context_tokens;
-- +goose StatementEnd
