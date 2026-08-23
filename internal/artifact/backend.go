// Package artifact is the content-addressed store for large tool outputs and
// immutable blobs. Bytes are stored once, keyed by
// SHA-256, outside SQLite; SQLite holds identity, metadata, and references.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Backend stores and retrieves content-addressed bytes.
type Backend interface {
	// Write persists data under a key derived from its content hash and returns
	// the storage key. Writes are atomic (temp file + rename) and hash-verified.
	Write(hash string, data []byte) (string, error)
	// Read returns the bytes for a storage key, verifying the content hash.
	Read(key string) ([]byte, error)
	// Exists reports whether a key is present.
	Exists(key string) bool
}

// FSBackend stores blobs on the filesystem as sha256/ab/cd/<full-hash>.
type FSBackend struct {
	root string
}

// NewFSBackend returns a filesystem backend rooted at dir.
func NewFSBackend(dir string) *FSBackend { return &FSBackend{root: dir} }

func (b *FSBackend) pathFor(hash string) string {
	// hash is a 64-char hex string; shard by first two byte pairs.
	return filepath.Join(b.root, "sha256", hash[0:2], hash[2:4], hash)
}

func (b *FSBackend) Write(hash string, data []byte) (string, error) {
	path := b.pathFor(hash)
	if _, err := os.Stat(path); err == nil {
		return hash, nil // already present (deduplicated)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("failed to create artifact dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp artifact: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("failed to write artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("failed to close artifact: %w", err)
	}
	// Verify the hash before publishing the file.
	if HashBytes(data) != hash {
		return "", fmt.Errorf("artifact hash mismatch on write")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("failed to publish artifact: %w", err)
	}
	return hash, nil
}

func (b *FSBackend) Read(key string) ([]byte, error) {
	data, err := os.ReadFile(b.pathFor(key))
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact: %w", err)
	}
	if HashBytes(data) != key {
		return nil, fmt.Errorf("artifact hash mismatch on read (corruption): %s", key)
	}
	return data, nil
}

func (b *FSBackend) Exists(key string) bool {
	_, err := os.Stat(b.pathFor(key))
	return err == nil
}

// HashBytes returns the hex SHA-256 of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
