package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	cacheSubdirTranspile = "transpile"
	cacheFileModule      = "module.go"
	cacheFileMeta        = "meta.json"
	cacheFileLock        = ".lock"
)

// cacheEnabled reports whether tier-1 transpile caching is enabled.
// The cache is disabled when the -cache flag is set to "off" or when
// the WASM2GO_RUN_CACHE environment variable is "0", "false", or "off"
// (case-insensitive).
func cacheEnabled(cfg Config) bool {
	if cfg.Cache == "off" {
		return false
	}
	switch strings.ToLower(os.Getenv("WASM2GO_RUN_CACHE")) {
	case "0", "false", "off":
		return false
	}
	return true
}

// cacheKeyVersion is a monotonic integer that must be bumped whenever
// the transpile post-processing logic changes, invalidating prior cache
// entries.
var cacheKeyVersion = 1

// wasm2goIdentity returns a string that uniquely identifies the wasm2go
// toolchain version (e.g. its module version or build stamp). It is a
// variable so tests can swap in a stub.
var wasm2goIdentity = func() string {
	return "dev"
}

// transpileCacheKey returns a deterministic cache key for the WASM
// module at wasmPath. The key incorporates the file contents, the
// wasm2go toolchain identity, and the cache-key version. If the file
// cannot be read, an empty string is returned.
func transpileCacheKey(wasmPath string) string {
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write(data)
	_, _ = fmt.Fprintf(h, "%s|%d", wasm2goIdentity(), cacheKeyVersion)
	return hex.EncodeToString(h.Sum(nil))
}

// transpileCacheMeta holds the metadata written alongside a cached
// transpilation result.
type transpileCacheMeta struct {
	Wasm2goID          string `json:"wasm2goID"`
	PostprocessVersion int    `json:"postprocessVersion"`
	WasmSize           int64  `json:"wasmSize"`
}

// currentTranspileCacheMeta returns metadata for a newly cached transpile
// entry using the active wasm2go identity and postprocess version.
func currentTranspileCacheMeta() transpileCacheMeta {
	return transpileCacheMeta{
		Wasm2goID:          wasm2goIdentity(),
		PostprocessVersion: cacheKeyVersion,
	}
}

// matchesCurrent reports whether meta reflects the active wasm2go identity
// and postprocess version.
func (m transpileCacheMeta) matchesCurrent() bool {
	return m.Wasm2goID == wasm2goIdentity() && m.PostprocessVersion == cacheKeyVersion
}

func readTranspileCacheMeta(dir string) (transpileCacheMeta, bool) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFileMeta))
	if err != nil {
		return transpileCacheMeta{}, false
	}
	var meta transpileCacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return transpileCacheMeta{}, false
	}
	return meta, true
}

func ensureTranspileCacheEntryDir(key string) (string, error) {
	dir := cacheTranspileEntryPath(key)
	return dir, os.MkdirAll(dir, 0755)
}

// removeTranspileCacheEntryDir deletes a tier-1 cache entry directory,
// discarding any lock file or partial artifacts from a failed population.
func removeTranspileCacheEntryDir(dir string) {
	_ = os.RemoveAll(dir)
}

// acquireExclusiveLock opens lockPath and acquires an exclusive flock. The
// returned unlock function releases the lock and closes the file.
func acquireExclusiveLock(lockPath string) (unlock func(), err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}

// withTranspileCachePopulateLock serializes cache population for key. The
// caller's populate runs only after an exclusive lock is acquired and a
// second cache lookup still misses. Concurrent waiters block until the
// writer finishes, then read the populated entry.
func withTranspileCachePopulateLock(key string, populate func() (string, error)) (string, error) {
	dir, err := ensureTranspileCacheEntryDir(key)
	if err != nil {
		return "", err
	}
	unlock, err := acquireExclusiveLock(filepath.Join(dir, cacheFileLock))
	if err != nil {
		return "", err
	}
	defer unlock()

	if src, hit := cacheGetTranspile(key); hit {
		return src, nil
	}
	src, err := populate()
	if err != nil {
		removeTranspileCacheEntryDir(dir)
		return "", err
	}
	return src, nil
}

// cachePutTranspile stores the transpiled Go source and its metadata in
// the tier-1 cache under the given key.
func cachePutTranspile(key, src string, meta transpileCacheMeta) error {
	dir, err := ensureTranspileCacheEntryDir(key)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, cacheFileModule), []byte(src), 0644); err != nil {
		return err
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, cacheFileMeta), metaJSON, 0644); err != nil {
		return err
	}
	return nil
}

// cacheGetTranspile retrieves the transpiled Go source for the given
// cache key. The second return value reports whether a cache hit
// occurred. A hit requires both the metadata and module files to exist
// and the metadata must match the current wasm2go identity and
// postprocess version.
func cacheGetTranspile(key string) (string, bool) {
	dir := cacheTranspileEntryPath(key)
	meta, ok := readTranspileCacheMeta(dir)
	if !ok || !meta.matchesCurrent() {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, cacheFileModule))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// cacheTranspileEntryPath returns the filesystem directory for the
// cache entry identified by key.
func cacheTranspileEntryPath(key string) string {
	return filepath.Join(cacheDir(), cacheSubdirTranspile, key)
}

// cacheDir returns the directory where wasm2go-run stores cached transpile
// artifacts. Resolution order:
//  1. WASM2GO_RUN_CACHE_DIR environment variable
//  2. XDG_CACHE_HOME/wasm2go-run
//  3. $HOME/.cache/wasm2go-run
func cacheDir() string {
	if dir := os.Getenv("WASM2GO_RUN_CACHE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "wasm2go-run")
	}
	return filepath.Join(os.Getenv("HOME"), ".cache", "wasm2go-run")
}
