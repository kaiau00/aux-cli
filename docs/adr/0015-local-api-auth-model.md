# ADR 0015 — Local API mutation/authentication model

- Status: accepted
- Date: 2026-08-14

## Decision

The local dashboard (`internal/dashboard`) is **read-only**: every route
(`/`, `/tasks`, `/sessions`, `/project`, `/memory`, `/impact`,
`/optimization`, `/api/v1/*`, `/api/snapshot`, `/api/sessions/*/messages`,
`/events`) only reads durable state; there is no mutation endpoint (no
POST/PUT/DELETE handler exists anywhere in `internal/dashboard/server.go` —
`handleSnapshot` explicitly rejects non-GET with 405). Authentication is a
single random 256-bit token (`randomToken()`, `crypto/rand`), generated fresh
per server start, required as either a `?token=` query parameter or an
`X-Aux-Dashboard-Token` header (`Server.authorized`). There is no user
system, no multi-token/multi-user model, and no persisted credential — the
token exists only in the running process and the printed dashboard URL
(`aux` CLI startup log).

Static sub-resources (`/css/*`, `/js/*`) are deliberately **not**
token-gated (`handleStaticAsset`'s doc comment: a browser can't attach a
custom header or query param to a `<link>`/`<script>` sub-resource request)
— this is safe specifically because they carry no data, only markup/styling
that reveals nothing about the user's project or sessions.

## Alternatives considered

- **Add mutation endpoints (e.g. approve a permission request, cancel a
  task) to the dashboard API.** Not adopted: every dashboard route reviewed
  this session (M4/M5 default-route rebuild, four new views) was kept
  read-only deliberately — mutation from a browser reachable at
  `127.0.0.1:<port>` raises CSRF-adjacent concerns (any local page/script
  could hit an unauthenticated-by-origin mutation endpoint) that a
  read-only API sidesteps entirely. If mutation is added later, it needs its
  own ADR addressing that.
- **Persistent, user-configured credentials (a password, an API key
  file).** Rejected: this is a localhost-bound, single-user, single-machine
  tool (`Options.Host` defaults to `127.0.0.1`) — a persisted credential
  adds setup friction and a secret to manage for a threat model (another
  local process/user on the same machine) that a fresh-per-run token already
  addresses adequately.
- **No authentication at all, relying solely on `127.0.0.1` binding.**
  Rejected: other local processes or browser tabs can still reach a
  loopback-bound port; the token is a real, cheap additional barrier against
  an unrelated local process reading session/task data.

## Evidence

- `internal/dashboard/server.go`'s route table (`Start`) and every
  `handleX` function were read in full this session while adding the four
  new views — none accepts a mutating verb.
- `Server.authorized` and `randomToken()` are exercised by
  `TestHandleIndexRequiresToken`/`TestHandleIndexAcceptsToken` and the
  per-view `Test*ViewRequiresToken` tests added alongside the M5 views.

## Consequences

- Any future feature that needs the dashboard to *act* (not just observe) —
  e.g. approving a permission request from the browser — is a deliberate,
  separate design decision, not a natural extension of the current API.
- The token is not a secret in the traditional sense (it's ephemeral,
  process-lifetime, and printed to the terminal that started `aux`); it must
  never be treated as suitable for exposure over a network the way a
  long-lived API key would need to be.

## Revisit trigger

Revisit if the dashboard is ever bound to a non-loopback host (multi-user or
remote-access deployment) — the current token model is explicitly sized for
"one trusted user, one machine" and would need real authentication (not just
a shared secret) before that changes.
