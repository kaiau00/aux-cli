package project

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// CanonicalizePath resolves a directory to an absolute, symlink-free path. If the
// path cannot be fully resolved (e.g. it does not exist yet) it falls back to the
// absolute form so identity remains stable.
func CanonicalizePath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

var scpLike = regexp.MustCompile(`^([^@/]+@)?([^:/]+):(.+)$`)

// NormalizeRemote canonicalizes a VCS remote URL without storing embedded
// credentials, so the same repository accessed over https/ssh with or without a
// username hashes to the same identity.
func NormalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	remote = strings.TrimSuffix(remote, ".git")

	// scp-like syntax: git@github.com:owner/repo
	if !strings.Contains(remote, "://") {
		if m := scpLike.FindStringSubmatch(remote); m != nil {
			host := strings.ToLower(m[2])
			path := strings.TrimPrefix(m[3], "/")
			return host + "/" + path
		}
		return remote
	}

	u, err := url.Parse(remote)
	if err != nil {
		return remote
	}
	host := strings.ToLower(u.Host)
	// Drop any userinfo (credentials).
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	path := strings.TrimPrefix(u.Path, "/")
	return host + "/" + path
}

// HashString returns a hex sha256 of s, or "" for empty input.
func HashString(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// RemoteHash returns the identity hash for a normalized remote.
func RemoteHash(remote string) string {
	return HashString(NormalizeRemote(remote))
}

// PathHash returns the identity hash for a canonical path.
func PathHash(path string) string {
	return HashString(path)
}

// nameFromRemoteOrPath derives a human-readable project name.
func nameFromRemoteOrPath(normalizedRemote, rootPath string) string {
	if normalizedRemote != "" {
		parts := strings.Split(normalizedRemote, "/")
		return parts[len(parts)-1]
	}
	return filepath.Base(rootPath)
}
