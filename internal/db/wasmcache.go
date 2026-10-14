package db

import (
	"math/bits"
	"os"
	"path/filepath"
	"sync"

	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/ncruces/go-sqlite3"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// This build has CGO_ENABLED=0 (see .goreleaser.yml), so SQLite is not a C
// library here: it is a WebAssembly module that wazero compiles to native code
// the first time a connection is opened. go-sqlite3 says as much -- "this is
// called implicitly when the first connection is openned, but is potentially
// slow" -- and measured on an M-series laptop it is 0.62s of every single
// start.
//
// wazero can keep the compiled result on disk, keyed by its own version, which
// takes that to 0.02s. Measured, three runs each: 0.62s cold, 0.61s with a cold
// cache, then 0.02s and 0.02s warm.
//
// The cache is a derived artifact: deleting it costs one slow start. It lives
// under the data directory rather than in a temporary directory so it is not
// swept between boots, and is written at 0o700 like the rest of that
// directory.
const wasmCacheDir = "wasm-cache"

var wasmCacheOnce sync.Once

// useCompiledSQLiteCache points go-sqlite3's wazero runtime at an on-disk
// compilation cache under dir. A failure is logged and ignored: the only cost
// is the slow compile it was meant to avoid.
//
// It has to run before the first connection, and only the first call can have
// any effect -- go-sqlite3 compiles the module once per process, under a
// sync.Once of its own.
func useCompiledSQLiteCache(dir string) {
	wasmCacheOnce.Do(func() {
		if sqlite3.RuntimeConfig != nil {
			// Somebody has already configured the runtime; theirs wins.
			return
		}
		cfg, err := compiledSQLiteCacheConfig(dir)
		if err != nil {
			logging.Warn("could not open the SQLite compilation cache; startup will be slower",
				"dir", dir, "error", err)
			return
		}
		sqlite3.RuntimeConfig = cfg
	})
}

// compiledSQLiteCacheConfig builds the wazero configuration, creating the cache
// directory. Separate from the one-shot installation above so it can be tested
// without depending on whether some earlier connection in the same process
// already fixed the runtime.
func compiledSQLiteCacheConfig(dir string) (wazero.RuntimeConfig, error) {
	path := filepath.Join(dir, wasmCacheDir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	cache, err := wazero.NewCompilationCacheWithDir(path)
	if err != nil {
		return nil, err
	}
	// go-sqlite3 applies these itself, but only when RuntimeConfig is nil, so
	// setting it means restating them. They are not optional: the memory limit
	// is what SQLite is given to work in, and SQLite's Wasm build needs the V2
	// core features.
	cfg := wazero.NewRuntimeConfig()
	if bits.UintSize < 64 {
		cfg = cfg.WithMemoryLimitPages(512) // 32MB
	} else {
		cfg = cfg.WithMemoryLimitPages(4096) // 256MB
	}
	return cfg.WithCoreFeatures(api.CoreFeaturesV2).WithCompilationCache(cache), nil
}
