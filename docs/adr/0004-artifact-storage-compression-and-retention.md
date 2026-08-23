# ADR 0004 — Artifact data directory, compression, and retention

- Status: accepted
- Date: 2026-08-14

## Decision

Large tool output and other immutable blobs are stored content-addressed on the
filesystem under `<data.directory>/artifacts/sha256/<hh>/<hh>/<full-hash>`
(`internal/artifact.FSBackend`), sharded by the first two byte-pairs of the
SHA-256 hash so no directory holds an unbounded number of entries. SQLite
holds identity, metadata (`storage_backend`, `storage_key`, `media_type`,
`byte_size`, `compression`, `sensitivity`, timestamps), and references;
bytes never live in SQLite. Writes are atomic (temp file + rename) and
hash-verified on both write and read, so a corrupted blob is detected rather
than silently served.

Compression and retention are **not implemented yet**: every artifact's
`compression` column is recorded as `"none"` (`artifact.Service` always sets
it), and there is no garbage-collection or TTL path — an artifact, once
written, is kept forever. This ADR records that as the current, deliberate
state (ship correctness and dedup first) rather than a gap discovered later.

## Alternatives considered

- **Compress on write (gzip/zstd).** Deferred: adds a decode step on every
  read path and a version field for the compression scheme, with no evidence
  yet that artifact storage is disk-constrained enough to justify it.
- **Store blobs in SQLite as BLOBs.** Rejected: defeats content-addressed
  dedup across tasks/branches (the whole point of a shared blob store) and
  bloats the primary database file that every other read touches.
- **Time- or size-based retention now.** Deferred: no data yet on real growth
  rates: it's better to instrument (`byte_size` is already recorded) and add
  retention once there's a real number to size a policy against, per §22
  "Database and artifact growth".

## Evidence

- Content addressing already gives free deduplication: `FSBackend.Write`
  short-circuits when the target path exists (`internal/artifact/backend.go`).
- The `compression` and `sensitivity` columns already exist in the schema
  (`internal/artifact/store.go`), so adding compression later is additive —
  no migration needed, just start writing a non-`"none"` value and branch on
  it at read time.

## Consequences

- Disk usage is unbounded until retention ships; operators relying on this
  for long-running installs should monitor `data.directory` size manually
  for now.
- Any future compression scheme must remain read-compatible with
  already-written `"none"` blobs (mixed compression states in one store).

## Revisit trigger

Add compression and/or retention once real artifact-directory size data from
actual usage shows it matters, or before this is packaged for multi-user /
long-lived server deployment (the database/artifact growth risk).
