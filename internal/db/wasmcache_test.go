package db

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// openForTest opens a connection with the production DSN.
func openForTest(path string) (*sql.DB, error) {
	return sql.Open("sqlite3", DSN(path))
}

// SQLite is a Wasm module in this build (CGO_ENABLED=0), compiled to native
// code on the first connection. This checks the on-disk compilation cache is
// written and, more importantly, that SQLite still works through the runtime
// configuration that enables it -- go-sqlite3 applies its own memory limit and
// core-feature settings only when RuntimeConfig is nil, so setting it means
// restating them, and getting that wrong breaks every query.
//
// Run in a child process: go-sqlite3 compiles the module once per process
// under a sync.Once, so only the first connection in a test binary can
// exercise a particular configuration, and nothing can guarantee this test is
// the one that gets there first.
func TestTheCompilationCacheIsWrittenAndSQLiteStillWorks(t *testing.T) {
	if dir := os.Getenv("AUX_WASM_CACHE_TEST_DIR"); dir != "" {
		runInChild(t, dir)
		return
	}

	dir := t.TempDir()
	for _, pass := range []string{"cold", "warm"} {
		cmd := exec.Command(os.Args[0],
			"-test.run=TestTheCompilationCacheIsWrittenAndSQLiteStillWorks")
		cmd.Env = append(os.Environ(), "AUX_WASM_CACHE_TEST_DIR="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s pass failed: %v\n%s", pass, err, out)
		}
		entries, err := os.ReadDir(filepath.Join(dir, wasmCacheDir))
		if err != nil {
			t.Fatalf("%s pass: the cache directory was not created: %v", pass, err)
		}
		if len(entries) == 0 {
			t.Fatalf("%s pass: the cache directory is empty, so nothing was cached", pass)
		}
	}
}

// runInChild is the body that runs in the child process: install the cache,
// then actually use SQLite through it.
func runInChild(t *testing.T, dir string) {
	t.Helper()
	useCompiledSQLiteCache(dir)
	if sqlite3.RuntimeConfig == nil {
		t.Fatal("the runtime configuration was not installed")
	}

	conn, err := sqlOpenAndQuery(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("SQLite does not work through the cached runtime: %v", err)
	}
	_ = conn
}

// sqlOpenAndQuery opens a database the way production does and runs enough SQL
// to prove the Wasm module is usable: a table, a write, and a read back.
func sqlOpenAndQuery(path string) (int, error) {
	conn, err := openForTest(path)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := conn.Exec(`CREATE TABLE t (n INTEGER)`); err != nil {
		return 0, err
	}
	if _, err := conn.Exec(`INSERT INTO t (n) VALUES (7)`); err != nil {
		return 0, err
	}
	var n int
	if err := conn.QueryRow(`SELECT n FROM t`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
